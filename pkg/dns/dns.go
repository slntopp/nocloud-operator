package dns

type DnsWrap struct {
	Network string
	DnsIp   string
}

func NewDnsWrap(network string, dnsIp string) *DnsWrap {
	return &DnsWrap{Network: network, DnsIp: dnsIp}
}
