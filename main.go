package main

import "log"
import "github.com/oschwald/geoip2-golang"

var geoip_country_maxmind_db *geoip2.Reader

func main() {
        // GeoIP for countries
	geoip_country_maxmind_db, err := geoip2.Open("/usr/share/GeoIP/GeoIP2-Country.mmdb")

        if err != nil {
                log.Fatalf("Can't open country mapping file: %v", err)
        }

        defer geoip_country_maxmind_db.Close()


	log.Printf("Hi")
}
