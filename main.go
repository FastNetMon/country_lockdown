package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/oschwald/geoip2-golang"
	"github.com/oschwald/maxminddb-golang"
)

var country_geoip_path = flag.String("geoip_path", "/usr/share/GeoIP/GeoIP2-Country.mmdb", "Path to GeoIP2 MMDB country data file")

func main() {
	flag.Parse()

	// GeoIP for countries
	geoip_country_maxmind_db, err := maxminddb.Open(*country_geoip_path)

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

	load_all_ipv4_networks_for_country(geoip_country_maxmind_db, "CN")
}

// Loads all networks for country with specific ISO code
// Luckily for us Hong Kong has HK code here and China has CN
func load_all_ipv4_networks_for_country(geoip_country_maxmind_db *maxminddb.Reader, country_iso_code string) error {
	// All fields https://github.com/oschwald/geoip2-golang/blob/main/reader.go#L139
	record := geoip2.Country{}

	// We use SkipAliasedNetworks because it's recommended in official documentation:
	// https://pkg.go.dev/github.com/oschwald/maxminddb-golang#SkipAliasedNetworks

	// Please note that a MaxMind DB may map IPv4 networks into several locations
	// in an IPv6 database. This iterator will iterate over all of these locations
	// separately. To only iterate over the IPv4 networks once, use the
	// SkipAliasedNetworks option.
	networks := geoip_country_maxmind_db.Networks(maxminddb.SkipAliasedNetworks)

	number_of_networks := 0

	for networks.Next() {
		subnet, err := networks.Network(&record)

		if err != nil {
			return fmt.Errorf("Cannot decode field in dataset: %v", err)
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

		// fmt.Printf("%s: %s\n", subnet.String(), record.Country.IsoCode)
		number_of_networks++
	}

	if networks.Err() != nil {
		return fmt.Errorf("Cannot correctly iterate over all available networks %w", networks.Err())
	}

	log.Printf("Successfully loaded %d prefixes for country %s", number_of_networks, country_iso_code)

	return nil
}
