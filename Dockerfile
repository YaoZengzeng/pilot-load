FROM golang:1.15 as builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOARCH=amd64 go build

FROM gcr.io/distroless/base-debian10
COPY --from=builder /src/pilot-load /usr/bin/
ENTRYPOINT ["/usr/bin/pilot-load"]
