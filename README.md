# SlowDNS, a deliberately slow DNS proxy

SlowDNS proxies DNS over UDP, with two configurable block levels.

1. **Hard**: Domains resolve to `0.0.0.0`, a la [pi-hole](https://pi-hole.net/).
1. **Soft**: After a 30 second delay, the domains resolve normally.

Hard blocking is useful to block trackers, ads, and other bloat.

Soft blocking is useful to retrain learned browsing habits for instant
gratification.

## Build, run

The server can be built and run with

```
go install github.com/spencer-p/slowdns
export PORT=53 DNSSERVERS=8.8.8.8 slowdns
```

See conf/ for example configuration for Kubernetes.

## Disclaimers

I built this as a means to an end. It is not high quality and you should not
depend on it.

- Not RFC compliant. My reference was Wireshark packet dumps.
- Potentially not your-home-network compliant.
- Reddit, Hacker News, and Instagram are hardcoded to soft block.
