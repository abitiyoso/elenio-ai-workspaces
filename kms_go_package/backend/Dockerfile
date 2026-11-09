FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/kms-api ./cmd/kms-api
RUN CGO_ENABLED=0 go build -o /out/kms-service-client ./cmd/kms-service-client
RUN CGO_ENABLED=0 go build -o /out/kms-admin ./cmd/kms-admin

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/kms-api /kms-api
COPY --from=build /out/kms-service-client /kms-service-client
COPY --from=build /out/kms-admin /kms-admin
COPY migrations /migrations
ENV MIGRATIONS_DIR=/migrations
EXPOSE 8080
ENTRYPOINT ["/kms-api"]
