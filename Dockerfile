FROM golang:1.23.0-alpine AS modules
WORKDIR /modules
ADD go.mod go.sum ./
RUN go mod download

FROM golang:1.22-alpine as builder
ARG SWAGGER=false
WORKDIR /app
COPY --from=modules /go/pkg /go/pkg
COPY . .
ENV SWAGGER=$SWAGGER
RUN if [ "$SWAGGER" = "true" ]; \
    then go install github.com/swaggo/swag/cmd/swag@v1.8.1 && \
    swag init; \
    fi;
RUN GOOS=linux go build -ldflags '-w -s' -a -o /app/application main.go

FROM golang:1.23.0-alpine
WORKDIR /app
COPY --from=builder /app/application /app/cosmos-indexer
