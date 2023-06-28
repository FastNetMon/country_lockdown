package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/netip"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	apb "google.golang.org/protobuf/types/known/anypb"

	apipb "github.com/osrg/gobgp/v3/api"

	"github.com/oschwald/geoip2-golang"
	"github.com/oschwald/maxminddb-golang"
	"go4.org/netipx"
)

type CountryLockdownConfiguration struct {
	GeoIPPath        string   `json:"geoip_path"`
	GoBGPApiAddress  string   `json:"gobgp_api_host"`
	CountryBlockList []string `json:"country_block_list"`
	IPAllowList      []string `json:"ip_allow_list"`
	BGPIPv4NextHop   string   `json:"bgp_ipv4_next_hop"`
}

var conf CountryLockdownConfiguration

func main() {
	conf_file_path := "/etc/country_lockdown.json"

	file_as_array, err := ioutil.ReadFile(conf_file_path)

	if err != nil {
		log.Fatalf("Could not read configuration file %s with error: %v", conf_file_path, err)
	}

	err = json.Unmarshal(file_as_array, &conf)

	if err != nil {
		log.Fatalf("Could not decode JSON configuration file %s: %v", conf_file_path, err)
	}

	// Unless specified in config use default value
	if conf.GeoIPPath == "" {
		conf.GeoIPPath = "/usr/share/GeoIP/GeoIP2-Country.mmdb"
	}

	// Unless specified in config use default value
	if conf.GoBGPApiAddress == "" {
		conf.GoBGPApiAddress = "[::1]:50051"
	}

	// GeoIP for countries
	geoip_country_maxmind_db, err := maxminddb.Open(conf.GeoIPPath)

	if err != nil {
		log.Fatalf("Can't open country mapping file: %v", err)
	}

	defer geoip_country_maxmind_db.Close()

	log.Printf("Loaded GeoIP file: %+v", geoip_country_maxmind_db.Metadata)

	// We need to be sure that database has correct type
	if geoip_country_maxmind_db.Metadata.DatabaseType != "GeoIP2-Country" {
		log.Fatalf("Wrong type of GeoIP database %s, please GeoIP2-Country type", geoip_country_maxmind_db.Metadata.DatabaseType)
	}

	log.Printf("GeoIP database has correct format")

	if conf.BGPIPv4NextHop == "" {
		log.Fatal("BGP IPv4 next hop is empty")
	}

	next_hop, err := netip.ParseAddr(conf.BGPIPv4NextHop)

	if err != nil {
		log.Fatalf("Cannot parse BGP IPv4 next hop %s: %v", conf.BGPIPv4NextHop, err)
	}

	if !next_hop.Is4() {
		log.Printf("Next hop must be IPv4 address")
	}

	log.Printf("Will use next hop: %s", next_hop)

	// https://pkg.go.dev/go4.org/netipx#IPSetBuilder
	// https://tailscale.com/blog/netaddr-new-ip-type-for-go/
	var b netipx.IPSetBuilder

	log.Printf("We have %d countries in country block list", len(conf.CountryBlockList))

	for _, country_code := range conf.CountryBlockList {
		log.Printf("Loading prefixes for country code: %s", country_code)

		country_prefix_list, err := load_all_ipv4_networks_for_country(geoip_country_maxmind_db, country_code)

		if err != nil {
			log.Printf("Cannot load prefixes for country: %v", err)
			continue
		}

		log.Printf("Successfully loaded %d prefixes which belong to this country", len(country_prefix_list))

		for _, prefix := range country_prefix_list {
			log.Printf("Prefix %s\n", prefix.String())

			b.AddPrefix(prefix)
		}

	}

	log.Printf("We have %d entries in allow list", len(conf.IPAllowList))

	// Exclude:
	for _, allow_ip := range conf.IPAllowList {
		log.Printf(allow_ip)

		addr, err := netip.ParseAddr(allow_ip)

		if err != nil {
			log.Printf("Cannot parse IP address %s", allow_ip)
			continue
		}

		// Exclude it from our ranges
		b.Remove(addr)
	}

	s, _ := b.IPSet()

	//fmt.Println(s.Ranges())

	prefixes_to_block := s.Prefixes()

	log.Printf("%d prefixes ready to announce", len(s.Prefixes()))

	for _, prefix := range prefixes_to_block {
		log.Printf("%s", prefix)
	}

	var opts []grpc.DialOption

	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	conn, err := grpc.Dial(conf.GoBGPApiAddress, opts...)

	if err != nil {
		log.Fatalf("Cannot connect to gRPC: %v", err)
	}

	defer conn.Close()

	log.Printf("Successfully connected to GoBGP")

	gobgp_client := apipb.NewGobgpApiClient(conn)

	for _, prefix := range prefixes_to_block {
		log.Printf("Prepare to announce %s", prefix)

		withdraw := false

		err = announce_prefix(gobgp_client, prefix, next_hop, withdraw)

		if err != nil {
			log.Printf("Cannot announce prefix %s: %v", prefix, err)
			continue
		}

		log.Printf("Successfully announced %s", prefix)
	}
}

// Announce prefix
func announce_prefix(gobgp_client apipb.GobgpApiClient, prefix netip.Prefix, next_hop netip.Addr, withdraw bool) error {

	nlri, err := apb.New(&apipb.IPAddressPrefix{
		Prefix:    prefix.Addr().String(),
		PrefixLen: uint32(prefix.Bits()),
	})

	// To check that we announce correct thing
	log.Printf("Announce %s/%d", prefix.Addr().String(), uint32(prefix.Bits()))

	if err != nil {
		return fmt.Errorf("Cannot create prefix message: %v", err)
	}

	origin_attr, err := apb.New(&apipb.OriginAttribute{
		Origin: 0,
	})

	if err != nil {
		return fmt.Errorf("Cannot create origin message: %v", err)
	}

	next_hop_attr, err := apb.New(&apipb.NextHopAttribute{
		NextHop: next_hop.String(),
	})

	if err != nil {
		return fmt.Errorf("Cannot create next hop message: %v", err)
	}

	// GoBGP will show them as {Communities: 0:100, 0:200}
	community_attribute, err := apb.New(&apipb.CommunitiesAttribute{
		Communities: []uint32{100, 200},
	})

	if err != nil {
		return fmt.Errorf("Cannot create community message: %v", err)
	}

	attrs := []*apb.Any{origin_attr, next_hop_attr, community_attribute}

	add_path_request := &apipb.AddPathRequest{
		Path: &apipb.Path{
			Family:     &apipb.Family{Afi: apipb.Family_AFI_IP, Safi: apipb.Family_SAFI_UNICAST},
			Nlri:       nlri,
			Pattrs:     attrs,
			IsWithdraw: withdraw,
		}}

	_, err = gobgp_client.AddPath(context.Background(), add_path_request)

	if err != nil {
		return fmt.Errorf("Cannot add path: %w", err)
	}

	log.Printf("Successfully announced prefix")

	return nil
}

// Loads all networks for country with specific ISO code
// Luckily for us Hong Kong has HK code here and China has CN
func load_all_ipv4_networks_for_country(geoip_country_maxmind_db *maxminddb.Reader, country_iso_code string) ([]netip.Prefix, error) {
	// All fields https://github.com/oschwald/geoip2-golang/blob/main/reader.go#L139
	record := geoip2.Country{}

	// We use SkipAliasedNetworks because it's recommended in official documentation:
	// https://pkg.go.dev/github.com/oschwald/maxminddb-golang#SkipAliasedNetworks

	// Please note that a MaxMind DB may map IPv4 networks into several locations
	// in an IPv6 database. This iterator will iterate over all of these locations
	// separately. To only iterate over the IPv4 networks once, use the
	// SkipAliasedNetworks option.
	networks := geoip_country_maxmind_db.Networks(maxminddb.SkipAliasedNetworks)

	prefix_list := []netip.Prefix{}

	for networks.Next() {
		subnet, err := networks.Network(&record)

		if err != nil {
			return nil, fmt.Errorf("Cannot decode field in dataset: %v", err)
		}

		// Filter out IPv6 networks
		// Well, it's IPNet and we need to use some fancy custom logic to find out type of it
		if subnet.IP.To4() == nil {
			continue
		}

		// TODO:
		// We do not expect private ranges here but we have to be sure
		// Well, I do not think that we have any functions to do so for prefixes
		// Skip for now

		// Check that network belongs to country we're interested in
		if record.Country.IsoCode != country_iso_code {
			continue
		}

		// Parse it into fancy netip.Prefix
		prefix, err := netip.ParsePrefix(subnet.String())

		if err != nil {
			log.Printf("Cannot parse %s as prefix with error %v", subnet.String(), err)
			// Well, we accept some malformed prefixes and do not return error in this case
			continue
		}

		prefix_list = append(prefix_list, prefix)
	}

	if networks.Err() != nil {
		return nil, fmt.Errorf("Cannot correctly iterate over all available networks %w", networks.Err())
	}

	// log.Printf("Successfully loaded %d prefixes for country %s", len(prefix_list), country_iso_code)

	return prefix_list, nil
}
