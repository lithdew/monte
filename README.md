# monte

[![MIT License](https://img.shields.io/apm/l/atomic-design-ui.svg?)](LICENSE)
[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lithdew/monte)
[![Discord Chat](https://img.shields.io/discord/697002823123992617)](https://discord.gg/HZEbkeQ)

The bare minimum for high performance, fully-encrypted RPC over TCP in Go.

## Features

1. Send requests, receive responses, or send messages without waiting for a response.
2. Send from 50MiB/s to 1500MiB/s, with zero allocations per sent message or RPC call.
3. Gracefully establish multiple client connections to a single endpoint up to a configurable limit.
4. Set the total number of connections that may concurrently be accepted and handled by a single endpoint.
5. Configure read/write timeouts, dial timeouts, handshake timeouts, or customize the handshaking protocol.
6. All messages, once the handshake protocol is complete, are encrypted and non-distinguishable from each other.
7. Supports graceful shutdowns for both client and server, with extensive tests for highly-concurrent scenarios.

## Protocol

### Handshake

1. Send X25519 curve point (32 bytes) to peer.
2. Receive X25519 curve point (32 bytes) from our peer.
3. Multiply X25519 curve scalar with X25519 curve point received from our peer.
4. Derive a shared key by using BLAKE-2b as a key derivation function over our scalar point multiplication result.
5. Encrypt further communication with AES 256-bit GCM using our shared key, with a nonce counter increasing for every
incoming/outgoing message.

### Message Format

1. Encrypted messages are prefixed with an unsigned 32-bit integer denoting the message's length.
2. The decoded message content is prefixed with an unsigned 32-bit integer designating a sequence number.
3. The sequence number is used as an identifier to identify requests/responses from one another.
4. The sequence number 0 is reserved for requests that do not expect a response.

## Benchmarks

```
$ cat /proc/cpuinfo | grep 'model name' | uniq
model name : Intel(R) Core(TM) i7-7700HQ CPU @ 2.80GHz

$ go test -bench=. -benchtime=10s
goos: linux
goarch: amd64
pkg: github.com/lithdew/monte
BenchmarkSend-8                          1814391              6690 ns/op         209.27 MB/s         115 B/op          0 allocs/op
BenchmarkSendNoWait-8                   10638730              1153 ns/op        1214.19 MB/s         141 B/op          0 allocs/op
BenchmarkRequest-8                        438381             28556 ns/op          49.03 MB/s         140 B/op          0 allocs/op
BenchmarkParallelSend-8                  4917001              2876 ns/op         486.70 MB/s         115 B/op          0 allocs/op
BenchmarkParallelSendNoWait-8           10317255              1291 ns/op        1084.78 MB/s         150 B/op          0 allocs/op
BenchmarkParallelRequest-8               1341444              8520 ns/op         164.32 MB/s         140 B/op          0 allocs/op
```