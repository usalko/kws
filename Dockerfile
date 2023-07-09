FROM golang:1.20.5-alpine

RUN apk add --update --no-cache alpine-sdk bash python3

# compile and install librdkafka
WORKDIR /root
RUN git clone -b master --single-branch https://github.com/edenhill/librdkafka.git
WORKDIR /root/librdkafka
# # checkout v1.1.0
# RUN git checkout 6160ec2
RUN ./configure
RUN make
RUN make install

# copy source files and private repo dep
COPY ./src/ /go/src/kws/
# COPY ./vendor/ /go/src/kws/vendor/

# static build the app
WORKDIR /go/src/kws
RUN go mod init
RUN go mod tidy
RUN go install

RUN go build -tags=static

# create final image
FROM alpine

COPY --from=0 /go/src/kws/kws /usr/bin/

# RUN apk --no-cache add \
#       cyrus-sasl \
#       openssl \

ENTRYPOINT ["kws"]
