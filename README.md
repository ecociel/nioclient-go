# Go Client for NIO Authorization

A Go client library for the **NIO Authorization Service**, a high-performance, relationship-based authorization system.
This library provides a gRPC client to interact with the service and includes middleware for easy integration
with the `julienschmidt/httprouter` framework.

# Usage

See the [cmd](cmd) directory for how to use with httprouter and for how to use the client.


# Updating gRPC Code

Run

    go generate ./...

to update the protobuf generated files.

The original proto file is located at [https://raw.githubusercontent.com/ecociel/nio-client/refs/heads/main/proto/iam.proto](https://raw.githubusercontent.com/ecociel/nio-client/refs/heads/main/proto/iam.proto)

# License

This project is licensed under the **Apache 2.0 License**. See the [LICENSE](https://www.google.com/search?q=LICENSE) file for details.




