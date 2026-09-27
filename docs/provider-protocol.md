# pinproc provider protocol

pinproc core does not contain vendor AI HTTP clients.

An AI provider is an independently packaged executable implementing protocol version 1. The core starts the provider executable for one logical decision request and communicates through JSON on standard input/output.

## Request

~~~json
{
  "protocol_version": 1,
  "action": "ask",
  "state": {},
  "questions": {},
  "config": {}
}
~~~

`config` contains provider-specific settings, including secrets. Providers must not require secrets in argv.

## Response

~~~json
{
  "protocol_version": 1,
  "answers": {}
}
~~~

On failure the provider returns the same protocol version with an `error` string. Stdout is reserved for the protocol; diagnostics belong on stderr.

Provider packages install a manifest under `/usr/share/pinproc/providers/<id>.yaml` and an executable under `/usr/libexec/pinproc/providers/<id>`.

`sudo pinproc setup ai` discovers these manifests and asks the user for provider settings. Adding or removing a provider does not require a pinproc core release.

Provider executables are trusted software installed by the machine administrator. They run as the same `pinproc` service user as the core. The core never downloads or executes arbitrary source code.

Removing all providers leaves pinproc operational with deterministic rules.

The core counts calls to the provider-neutral decision interface. The normal budget permits up to eight logical AI decision calls. A provider may perform transport retries inside one logical call.