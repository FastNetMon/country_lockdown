package main

import "log"
import "flag"
import "github.com/oschwald/geoip2-golang"

var geoip_country_maxmind_db *geoip2.Reader

var country_geoip_path = flag.String("geoip_path", "/usr/share/GeoIP/GeoIP2-Country.mmdb", "Path to GeoIP2 MMDB country data file")

func main() {
	flag.Parse()

        // GeoIP for countries
	geoip_country_maxmind_db, err := geoip2.Open(*country_geoip_path)

        if err != nil {
                log.Fatalf("Can't open country mapping file: %v", err)
        }

        defer geoip_country_maxmind_db.Close()


	log.Printf("Hi")
}
