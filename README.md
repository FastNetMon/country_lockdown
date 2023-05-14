Run BGP daemon:

sudo ~/Documents/gobgpd  --log-level=9 --config-file=gobgpd.conf

Check announced prefixes:

~/Documents/gobgp global rib -a ipv4

Packages:

Install: https://nfpm.goreleaser.com/install/

wget https://github.com/osrg/gobgp/releases/download/v3.14.0/gobgp_3.14.0_linux_amd64.tar.gz -O/tmp/gobgp.tar.gz
tar -xf /tmp/gobgp.tar.gz  -C bin/

nfpm pkg --packager deb --target bin/

nfpm pkg --packager rpm --target bin/
