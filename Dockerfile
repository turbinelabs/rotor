FROM phusion/baseimage:0.10.0

# upgrade/install deps
RUN apt-get update
RUN DEBIAN_FRONTEND="noninteractive" apt-get upgrade -y
RUN DEBIAN_FRONTEND="noninteractive" apt-get install -y git

# install go
RUN curl -s -L -O https://storage.googleapis.com/golang/go1.10.1.linux-amd64.tar.gz
RUN tar -C /usr/local -xzf go1.10.1.linux-amd64.tar.gz
ENV GOPATH /go
ENV PATH "$PATH:/usr/local/go/bin:$GOPATH/bin"

# Add src
ADD . $GOPATH/src/github.com/turbinelabs/rotor

# Get go deps
RUN go get github.com/turbinelabs/rotor/...

# Install binary
RUN go install github.com/turbinelabs/rotor/...
RUN mv $GOPATH/bin/rotor /usr/local/bin/rotor

# cleanup go
RUN rm -rf /usr/local/go
RUN rm -rf /go/src

# cleanup git
RUN DEBIAN_FRONTEND="noninteractive" apt-get remove -y git
RUN DEBIAN_FRONTEND="noninteractive" apt-get autoremove -y

# Clean up APT when done.
RUN apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# Add support files
ADD rotor.sh /usr/local/bin/rotor.sh
RUN chmod +x /usr/local/bin/rotor.sh

# best guess
EXPOSE 50000

# Use baseimage-docker's init system.
CMD ["/sbin/my_init", "--", "/usr/local/bin/rotor.sh"]
