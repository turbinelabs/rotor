FROM golang:stretch as builder

ENV GOPATH /go
ENV PATH "$PATH:/usr/local/go/bin:$GOPATH/bin"

# Add src
COPY . $GOPATH/src/github.com/turbinelabs/rotor

# Get go deps
RUN go get github.com/turbinelabs/rotor/...

# Install binaries
RUN CGO_ENABLED=0 GOOS=linux go install github.com/turbinelabs/rotor/...

# Production image
FROM alpine:latest

# Upgrade and apk cleanup
RUN apk -U upgrade && rm /var/cache/apk/*

# Copy binaries and scripts
COPY --from=builder /go/bin/rotor* /usr/local/bin/
COPY rotor.sh /usr/local/bin/rotor.sh
RUN chmod +x /usr/local/bin/rotor.sh

# best guess
EXPOSE 50000

ENTRYPOINT ["/usr/local/bin/rotor.sh"]
