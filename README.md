# Build

Run `make build` to build the application. Executable will be built to `build/ubft`. 

### Build dependencies

* [`Go`](https://go.dev/doc/install) version 1.24.
* `C` compiler, recent versions of [GCC](https://gcc.gnu.org/) are recommended. In Debian and Ubuntu repositories, GCC is part of the build-essential package. On macOS, GCC can be installed with [Homebrew](https://formulae.brew.sh/formula/gcc).

# Money Partition

1. Run script `./setup-nodes.sh -m 3 -t 0` to generate configuration for a root chain and 3 money partition nodes.
    The script generates root chain and partition node keys, genesis files.
    Node configuration files are located in `test-nodes` directory.
   * Initial bill owner predicate can be specified with flag `-i predicate-in-hex`.
2. Run script `./start.sh -r -p money` to start root chain and 3 money partition nodes
3. Run script `./stop.sh -a` to stop the root chain and partition nodes.
   
   Alternatively, use `stop.sh` to stop any partition or root and `start.sh` to resume. See command help for more details. 

# User Token Partition

Typical set-up would run money and user token partition as fee credits need to be added to the user token partition
in order to pay for transactions.
Theoretically it is also possible run only the user token partition on its own, but it would not make much sense.
1. Run script `./setup-nodes.sh -m 3 -t 3` to generate configuration for a root chain and 3 money and token partition nodes.
   The script generates root chain and partition node keys, genesis files.
   Node configuration files are located in `test-nodes` directory.
2. Run script `./start.sh -r -p money -p tokens` to start root chain and 3 partition nodes (money and token)
3. Run script `./stop.sh -a` to stop the root chain and partition nodes.

# Start all partitions at once

1. Run script `./setup-nodes.sh` to generate genesis for root, and 3 money and tokens nodes.
2. Run `./start.sh -r -p money -p tokens` to start everything
3. Run `./stop.sh -a` to stop everything

# Configuration

It's possible to define the configuration values from (in the order of precedence):

* Command line flags (e.g. `--address="/ip4/127.0.0.1/tcp/26652"`)
* Environment (Prefix 'AB' must be used. E.g. `UBFT_ADDRESS="/ip4/127.0.0.1/tcp/26652"`)
* Configuration file (properties file) (E.g. `address="/ip4/127.0.0.1/tcp/26652"`)
* Default values

The default location of configuration file is `$UBFT_HOME/config.props`

The default `$UBFT_HOME` is `$HOME/.ubft`

## Logging configuration

Logging can be configured through a yaml configuration file. See [logger-config.yaml](cli/ubft/config/logger-config.yaml) for example.

Default location of the logger configuration file is `$UBFT_HOME/logger-config.yaml`

The location can be changed through `--logger-config` configuration key. If it's relative URL, then it's relative
to `$UBFT_HOME`. Some logging related parameters can be set via command line parameters too - run `ubft -h`
for more.

See [logging.md](./docs/logging.md) for information about log schema.

# Distributed Tracing

To enable tracing environment variable `UBFT_TRACING` (or command line flag `--tracing`) has
to be set to one of the supported exporter names: `stdout`, `otlptracehttp` or `zipkin`.

Exporter can be further configured using
[General SDK Configuration](https://opentelemetry.io/docs/concepts/sdk-configuration/general-sdk-configuration/)
and exporter specific
[OpenTelemetry Protocol Exporter](https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/exporter.md)
environment variables.
Exceptions are:

- instead of `OTEL_TRACES_EXPORTER` and `OTEL_METRICS_EXPORTER` we use specific
  environment variables (ie `UBFT_TRACING`) or command line flags to select the exporter;
- propagators are set (`OTEL_PROPAGATORS`)  to “tracecontext,baggage”;
- `OTEL_SERVICE_NAME` is set based on "best guess" of current binary's role (ie to
  "ubft.wallet", "ubft.tokens", "ubft.money",...)

## Tracing tests

To enable trace exporter for test the `UBFT_TEST_TRACER` environment variable has to be set
to desired exporter name, ie

```sh
UBFT_TEST_TRACER=otlptracehttp go test ./...
```

The test tracing will pick up the same OTEL environment variables linked above except that
some parameters are already "hardcoded":

- "always_on" sampler is used (`OTEL_TRACES_SAMPLER`);
- the `otlptracehttp` exporter is created with "insecure client transport"
  (`OTEL_EXPORTER_OTLP_INSECURE`);

# Generate tests for Rust SDK

If the `UBFT_RUST_SDK_ROOT` environment variable is set (not empty) and points to
existing directory some tests will generate tests for the Rust predicate SDK.

To generate tests for the Rust predicate SDK run
```sh
UBFT_RUST_SDK_ROOT="/path/to/rust-predicates-sdk" go test ./...
```

# Set up autocompletion

To use autocompletion (supported with `bash`, `fish`, `powershell` and `zsh`), run the following commands after
building (this is `bash` example):

* `./ubft completion bash > /tmp/completion`
* `source /tmp/completion`

# CI setup

# Build Docker image with local dependencies

For example, if go.work is defined as follows:

```plain
go 1.23.2

use (
    .
    ../bft-go-base
)
```

The folder can add be added into Docker build context by specifying it as follow:

```console
DOCKER_GO_DEPENDENCY=../bft-go-base make build-docker
```
