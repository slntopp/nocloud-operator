package dns

import (
	"context"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc/credentials/insecure"

	"github.com/slntopp/nocloud-proto/dns"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

type DnsWrap struct {
	Network   string
	DnsIp     string
	DnsClient dns.DNSClient

	log *zap.Logger
}

func NewDnsWrap(log *zap.Logger, network, dnsIp, dnsMgmtHost string) *DnsWrap {
	port := os.Getenv("DNS_MGMT_PORT")
	if port == "" {
		port = "8000"
	}

	host := fmt.Sprintf("%s:%s", dnsMgmtHost, port)

	conn, err := grpc.Dial(host, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err.Error())
	}

	dnsClient := dns.NewDNSClient(conn)
	return &DnsWrap{Network: network, DnsIp: dnsIp, DnsClient: dnsClient, log: log}
}

func (d *DnsWrap) Get(ctx context.Context, zoneName string, ip string, aValue string) error {
	log := d.log.Named("get")

	zone := dns.Zone{Name: zoneName}
	get, err := d.DnsClient.Get(ctx, &zone)
	if err != nil {
		return err
	}

	if get.Locations == nil {
		get.Locations = make(map[string]*dns.Record)
	}

	_, ok := get.Locations[aValue]
	if !ok {
		get.Locations[aValue] = &dns.Record{A: make([]*dns.Record_A, 1), Txt: make([]*dns.Record_TXT, 1)}
	}

	location := get.Locations[aValue]
	if location.A[0] == nil {
		location.A[0] = &dns.Record_A{}
	}
	if location.Txt[0] == nil {
		location.Txt[0] = &dns.Record_TXT{}
	}
	location.A[0].Ip = ip
	location.A[0].Ttl = 300
	location.Txt[0] = &dns.Record_TXT{Text: "Was changed by operator at " + time.Now().UTC().String(), Ttl: 300}
	get.Locations[aValue] = location

	put, err := d.DnsClient.Put(ctx, get)
	if err != nil {
		return err
	}

	log.Info("Put DNS Record", zap.Int64("result", put.Result))
	return nil
}
