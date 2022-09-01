# Labels for dns management and configuration

__nocloud.dns.server__ - label of dns server. Added with __nocloud.dns.network__ to set network, where we should find his ip.

__nocloud.dns.api__ - laber of dns-mgmt server.

__nocloud.dns.required__ - added to services, that we shoud configure with dns server ip.

__nocloud.dns.zone__, __nocloud.dns.key.a__, __nocloud.dns.key.aaaa__, __nocloud.dns.key.cname__, __nocloud.dns.key.txt__ - labels for generating records in dns-mgmt service  