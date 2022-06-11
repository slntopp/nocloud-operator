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

const Port = "8080"

func NewDnsWrap(network string, dnsIp string, dnsMgmtIp string) *DnsWrap {
	host := dnsIp + ":" + Port
	conn, err := grpc.Dial(host, grpc.WithBlock())
	if err != nil {
		log.Fatal("Something bad with dns client")
	}

	dnsClient := proto.NewDNSClient(conn)
	return &DnsWrap{Network: network, DnsIp: dnsIp, DnsClient: dnsClient}
}
