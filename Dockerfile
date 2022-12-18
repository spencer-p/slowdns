FROM golang:1.19 AS builder

WORKDIR /
COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 go build -v .

FROM gcr.io/distroless/base:nonroot

COPY --from=builder /slowdns /slowdns

EXPOSE 8053/udp

ENTRYPOINT ["/slowdns"]
