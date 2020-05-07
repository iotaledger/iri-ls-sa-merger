
FROM ubuntu:19.10

RUN apt-get update && apt-get install -y --no-install-recommends \
        libgflags-dev libsnappy-dev zlib1g-dev libbz2-dev libzstd-dev liblz4-dev \
        ca-certificates wget git build-essential make \
    && rm -rf /var/lib/apt/lists/*

# Installing Go
RUN wget -P /tmp https://dl.google.com/go/go1.13.6.linux-amd64.tar.gz
RUN tar -C /usr/local -xzf /tmp/go1.13.6.linux-amd64.tar.gz
RUN rm /tmp/go1.13.6.linux-amd64.tar.gz

ENV GOPATH /go
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH

RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 777 "$GOPATH"

# Installing iri-ls-sa-merger
RUN git clone -b 5.17.fb https://github.com/facebook/rocksdb
RUN cd rocksdb && make -j 5 DISABLE_WARNING_AS_ERROR=true shared_lib && make -j 5 DISABLE_WARNING_AS_ERROR=true install-shared

ENV LD_LIBRARY_PATH=/usr/local/lib

ENV CGO_CFLAGS="-I/usr/local/include/rocksdb"
ENV CGO_LDFLAGS="-L/usr/local/lib -lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy -llz4 -lzstd"

COPY . .

RUN go get github.com/tecbot/gorocksdb
RUN go install .

ENV $PATH=$PATH:/go/bin

WORKDIR /data

ENTRYPOINT [ "iri-ls-sa-merger" ]
