FROM golang:1.10.3 AS golang

# Add src
ADD . /go/src/github.com/turbinelabs/rotor

# Get go deps
RUN go get github.com/turbinelabs/rotor/...

# Install binaries
RUN go install github.com/turbinelabs/rotor/...

FROM phusion/baseimage:0.10.2

# Clean up APT when done.
RUN apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# Add support files
COPY --from=golang /go/bin/rotor* /usr/local/bin/
ADD rotor.sh /usr/local/bin/rotor.sh
RUN chmod +x /usr/local/bin/rotor.sh

# best guess
EXPOSE 50000

# Use baseimage-docker's init system.
CMD ["/sbin/my_init", "--", "/usr/local/bin/rotor.sh"]
