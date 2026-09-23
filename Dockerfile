FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/closeview ./cmd/closeview

FROM alpine:3.22
COPY --from=build /out/closeview /usr/local/bin/closeview
ENV HOME=/tmp
USER 1000:1000
ENTRYPOINT ["closeview"]
CMD ["serve", "--host", "127.0.0.1", "--port", "3434"]
