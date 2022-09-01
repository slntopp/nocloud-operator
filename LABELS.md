# Configuration Labels supported by this Operator

## Update container Images

Adding `nocloud.update` label to container will make Operator check for the new Image under same tag every N seconds(configurable in `operator-config.yml`).

> **Note:**  
Most of NoCloud Core and Drivers containers are already labeled with `nocloud.update`

## DNS Management

If you have Coredns and `dns-mgmt` service set up you could use Operators help to maintain internal DNS. The following container types and labels are available:

1. DNS server(Coredns or alike).

    * Must be marked with `nocloud.dns.server` and so Operator "remembers" it's IP address
    * The DNS docker network must be specified using `nocloud.dns.network` label(like `nocloud.dns.network=dns`). DNS Servers IP will be taken from this network.

2. DNS Management API.

    * Must be marked with `nocloud.dns.api` label, so can store the records into DNS server using NoCloud DNS-mgmt API.

3. Container using the local DNS.

    * Must have `nocloud.dns.required` label, so Operator would recreate it and add DNS Server IP address along with default DNS servers IPs to its DNS Config.

4. Container Internal Domain

    You can add internal DNS records, using the following set of labels:

    * `nocloud.dns.network` - same as for DNS server, IP address will be taken from specified network (and used in record)
    * `nocloud.dns.zone` - first and second level domains to add record into (for examle `internal.nocloud`)
    * `nocloud.dns.key.[a|aaa|cname|txt]` - type of record and its value

    Example. Let's say we want a container doing, let's say `analytics`, to be resolvable internally under name `analytics.internal.nocloud`, then `labels` section of docker compose file would be looking like:

    ```yaml
    labels:
        - nocloud.dns.network=default
        - nocloud.dns.zone=internal.nocloud
        - nocloud.dns.key.a=analytics
        - nocloud.dns.key.txt=Example
    ```

    Let's assume container IP at the moment is `172.0.1.10`. This configuration will produce following records

    Domain: `internal.nocloud`
    | Type | Value | TTL |
    |:----:| ----- | --- |
    | A | analytics | 300|
    | TXT | Example | 300|
    | TXT | Was changed by operator at 2022-09-01 14:00:55.160986678 +0000 UTC | 0|
