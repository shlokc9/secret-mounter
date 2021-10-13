FROM golang:1.17
LABEL PROJECT="secret-sneaker"

COPY src /go/src/secret-sneaker/
WORKDIR /go/src/secret-sneaker/

RUN go get -d

RUN go install

RUN mkdir -p bin
RUN go build -o bin/

ENTRYPOINT ["/go/src/secret-sneaker/bin/secret-sneaker"]