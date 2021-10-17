FROM golang as builder

WORKDIR /app
COPY ./*.go ./go.* ./

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o start .

FROM ubuntu

WORKDIR /app
COPY --from=builder /app/start /app/

ENTRYPOINT ["/app/start"]
