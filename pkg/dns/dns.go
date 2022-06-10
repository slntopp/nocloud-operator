package dns

import (
	"github.com/gorobot-nz/docker-operator/pkg/dns/proto"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type DnsWrap struct {
	Network   string
	DnsIp     string
	DnsClient proto.DNSClient
}

func NewDnsWrap(network string, dnsIp string) *DnsWrap {
	host := "127.0.0.1:8080"
	conn, err := grpc.Dial(host, grpc.WithBlock())
	if err != nil {
		log.Fatal("Something bad with dns client")
	}

	dnsClient := proto.NewDNSClient(conn)
	return &DnsWrap{Network: network, DnsIp: dnsIp, DnsClient: dnsClient}
}
