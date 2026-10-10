# Luban

Luban is a Go framework for building AI applications with a small, consistent
Agent API. Business modules provide their model gateway adapter, business
functions, Tools, and Skills; the SDK owns orchestration and Eino integration.

The public SDK module is `github.com/lubanAgent/luban/sdk`, with package name
`luban`. The Runtime, registration, sequential Chain, business functions, Eino
ChatModelAgent, Tools, Skills, structured OutputTool, Batch, and HTTP/MQ/Cron
adapters are implemented. The admissions SchoolPicker stage now runs through
Luban, with OpenProxy HTTP kept in the admissions `ModelInvoker`.

The repository root provides one SDK validation command:

```sh
make check-sdk
```

It runs formatting, module tidiness, unit tests, race tests, `go vet`, and a
dependency-boundary check. The standalone SDK depends only on Go's standard
library and open-source modules; it does not import company packages.

The SDK uses the V3 contract: business modules provide a `ModelInvoker` that
performs model HTTP requests, register functions/Tools/Skills, then call
`Runtime.Run(ctx, appKey, input)`. The Runtime adapts those calls to Eino while
keeping business gateway and side-effect code in the host module. For the
current API and scope, see the V3 design documents.

## Offline example

The executable fake-model example exercises `PrepareReview → Agent → SaveReview`
without a gateway, company credentials, or network access:

```sh
cd sdk
go test -run ExampleRuntime_Run
```

The full example lives in [`sdk/example_test.go`](sdk/example_test.go) and is
compiled and run by the normal test suite. It registers a business preparation
function, sends its `Request.Messages` to a configured Agent, then passes the
Agent's raw text result to the next business function.

The module path is `github.com/lubanAgent/luban/sdk`. A published install
version will be documented after the first SDK release. Admissions currently
uses a local `replace` to the sibling Luban checkout until the first release.

The admissions adapter passed fake-client tests and one live SchoolPicker
request. The live test is opt-in:

```sh
ADMISSIONS_LIVE_LUBAN_TESTS=1 go test ./service/ai/lubanruntime \
  -run '^TestLiveSchoolPickerLubanOpenProxy$' -count=1
```

The live smoke test uses synthetic input and does not run the database-backed
strategy pipeline.
