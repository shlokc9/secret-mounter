FROM golang:1.17
LABEL PROJECT="secret-mounter"

COPY src /go/src/secret-mounter/
WORKDIR /go/src/secret-mounter/

RUN go get -d

RUN go install

RUN mkdir -p bin
RUN go build -o bin/

ENTRYPOINT ["/go/src/secret-mounter/bin/secret-mounter"]
