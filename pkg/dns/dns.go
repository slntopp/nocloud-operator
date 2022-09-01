package dns

import (
	"context"
	"google.golang.org/grpc/credentials/insecure"
	"os"
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
	host := os.Getenv("DNS_MGMT_HOST")
	conn, err := grpc.Dial(host, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err.Error())
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

	_, ok := get.Locations[aValue]
	if !ok {

		if get.Locations == nil {
			get.Locations = make(map[string]*proto.Record)
		}

		get.Locations[aValue] = &proto.Record{A: make([]*proto.Record_A, 1), Txt: make([]*proto.Record_TXT, 1)}
	}

	location := get.Locations[aValue]
	if location.A[0] == nil {
		location.A[0] = &proto.Record_A{}
	}
	if location.Txt[0] == nil {
		location.Txt[0] = &proto.Record_TXT{}
	}
	location.A[0].Ip = ip
	location.A[0].Ttl = 300
	location.Txt[0] = &proto.Record_TXT{Text: "Was changed by operator at " + time.Now().UTC().String()}
	get.Locations[aValue] = location

	put, err := d.DnsClient.Put(ctx, get)
	if err != nil {
		return err
	}

	log.Info("Put DNS Record", zap.Int64("result", put.Result))
	return nil
}
