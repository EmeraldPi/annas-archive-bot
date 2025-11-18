FROM golang:1.20 AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bot ./main.go

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build /app/bot /app/bot
ENV TOKEN=""
ENTRYPOINT ["/app/bot"]