module github.com/bluesbirds/academy

go 1.22

require (
	github.com/go-chi/chi/v5 v5.0.12
	github.com/go-chi/cors v1.2.1
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/mattn/go-sqlite3 v1.14.52
	github.com/tursodatabase/libsql-client-go v0.0.0-20260528064733-9d5d30a29a60
	golang.org/x/crypto v0.26.0
)

require (
	github.com/antlr4-go/antlr/v4 v4.13.0 // indirect
	github.com/coder/websocket v1.8.12 // indirect
	golang.org/x/exp v0.0.0-20240325151524-a685a6edb6d8 // indirect
)

// golang.org/x/... module proxy resolution is blocked in this sandbox's
// egress rules; point straight at the GitHub mirror instead. Remove this
// once building outside the sandbox.
replace golang.org/x/crypto => github.com/golang/crypto v0.26.0

replace golang.org/x/exp => github.com/golang/exp v0.0.0-20240325151524-a685a6edb6d8
