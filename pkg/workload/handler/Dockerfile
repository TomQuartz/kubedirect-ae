# Stage 0: Build #
# Use the official Golang image to create a build artifact.
# This is based on Debian and sets the GOPATH to /go.
FROM golang:1.21.5 as BUILDER

ARG TARGET_PLATFORM=knative

# Create and change to the app directory.
WORKDIR /app

# Copy local code to the container image.
COPY . ./

# Retrieve application dependencies using go modules.
# Allows container builds to reuse downloaded dependencies.
RUN go mod download

# Build the binary.
WORKDIR /app/cmd/workload/${TARGET_PLATFORM}

# -mod=readonly: ensures immutable go.mod and go.sum in container builds.
# CGO_ENABLED=1: uses common libraries found on most major OS distributions.
# GOARCH=amd64 GOGCCFLAGS=-m64: specifies x86, 64-bit GCC.
RUN CGO_ENABLED=1 GOARCH=amd64 GOGCCFLAGS=-m64 GOOS=linux go build -mod=readonly -v -o /server main.go

# Stage 1: Run #
FROM debian:stable-slim

# Copy the binary to the production image from the BUILDER stage.
COPY --from=BUILDER /server /server

# Run the web service on container startup.
ENTRYPOINT ["/server"]

LABEL org.opencontainers.image.source=https://github.com/tomquartz/kubedirect-bench