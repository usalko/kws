FROM golang:1.21.0-alpine AS build

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
# RUN go mod init
RUN go mod tidy
RUN go install -tags=musl

RUN go build -tags=musl -tags=dynamic

# SHOW CONTENT FROM BUILD FOLDER
RUN ls -la /go/src/kws

# create final image
FROM alpine:3.18.3 AS runtime

COPY --from=build /go/src/kws/main /usr/bin/kws
COPY --from=build /usr/local /usr/local

# RUN apk --no-cache add \
#       cyrus-sasl \
#       openssl \

ENTRYPOINT ["kws"]
