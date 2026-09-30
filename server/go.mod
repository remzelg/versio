module github.com/remycarr/versio/server

go 1.22

require github.com/remycarr/versio/client v0.0.0

// The client module lives beside this one during development. Once it is
// published on its own, drop this line and require a tagged version.
replace github.com/remycarr/versio/client => ../client
