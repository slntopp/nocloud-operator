package dns

import (
	"context"
	log "github.com/sirupsen/logrus"
	"github.com/slntopp/nocloud/pkg/dns/proto"
	"google.golang.org/grpc"
)

type DnsWrap struct {
	Network   string
	DnsIp     string
	DnsClient proto.DNSClient
}

func NewDnsWrap(network string, dnsIp string, dnsMgmtIp string) *DnsWrap {
	host := dnsMgmtIp + ":8080"
	conn, err := grpc.Dial(host, grpc.WithBlock())
	if err != nil {
		log.Fatal("Something bad with dns client")
	}

	dnsClient := proto.NewDNSClient(conn)
	return &DnsWrap{Network: network, DnsIp: dnsIp, DnsClient: dnsClient}
}

func (d *DnsWrap) Get(ctx context.Context, zoneName string, ip string, locations map[string]string) error {
	zone := proto.Zone{Name: zoneName}
	get, err := d.DnsClient.Get(ctx, &zone)
	if err != nil {
		return err
	}

	locs := get.Locations

	for key, value := range locations {
		records := locs[key]

		switch value {
		case "a":
			records.A = append(records.A, &proto.Record_A{Ip: ip})
		case "aaaa":
			records.Aaaa = append(records.Aaaa, &proto.Record_AAAA{Ip: ip})
		case "cname":
			records.Cname = append(records.Cname, &proto.Record_CNAME{Host: ip})
		case "txt":
			records.Txt = append(records.Txt, &proto.Record_TXT{Text: ip})
		}

		locs[key] = records
	}

	get.Locations = locs

	put, err := d.DnsClient.Put(ctx, get)
	if err != nil {
		return err
	}

	log.Printf("From dns log %d", put.Result)

	return nil
}
