FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api && CGO_ENABLED=0 go build -o /out/worker ./cmd/worker && CGO_ENABLED=0 go build -o /out/migrate ./cmd/migrate
FROM alpine:3.23
RUN addgroup -S abt && adduser -S -G abt abt
COPY --from=build /out/ /app/
USER abt
EXPOSE 8080
CMD ["/app/api"]
