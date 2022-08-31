package dns

import (
	"context"
	"time"

	"github.com/slntopp/nocloud/pkg/dns/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

type DnsWrap struct {
	Network   string
	DnsIp     string
	DnsClient proto.DNSClient

	log *zap.Logger
}

func NewDnsWrap(log *zap.Logger, network, dnsIp string) *DnsWrap {
	host := "dns-mgmt:8000"
	conn, err := grpc.Dial(host, grpc.WithBlock())
	if err != nil {
		log.Fatal("Something bad with dns client")
	}

	dnsClient := proto.NewDNSClient(conn)
	return &DnsWrap{Network: network, DnsIp: dnsIp, DnsClient: dnsClient, log: log}
}

func (d *DnsWrap) Get(ctx context.Context, zoneName string, ip string, aValue string) error {
	log := d.log.Named("get")

	zone := proto.Zone{Name: zoneName}
	get, err := d.DnsClient.Get(ctx, &zone)
	if err != nil {
		return err
	}

	location, ok := get.Locations[aValue]
	if !ok {
		get.Locations[aValue] = &proto.Record{A: make([]*proto.Record_A, 1), Txt: make([]*proto.Record_TXT, 0)}
	}

	location.A[0].Ip = ip
	location.A[0].Ttl = 300
	location.Txt = append(location.Txt, &proto.Record_TXT{Text: "Was changed by operator at " + time.Now().UTC().String()})
	get.Locations[aValue] = location

	put, err := d.DnsClient.Put(ctx, get)
	if err != nil {
		return err
	}

	log.Info("Put DNS Record", zap.Int64("result", put.Result))
	return nil
}
