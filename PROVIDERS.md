# pinproc providers

The pinproc core does not contain vendor-specific AI integrations.

A provider is a separately installed adapter with two pieces:

~~~text
/usr/share/pinproc/providers/<id>.yaml
/usr/libexec/pinproc/providers/<executable>
~~~

The manifest is static metadata. pinproc reads it without executing provider code.

## Manifest

~~~yaml
protocol_version: 1
id: example
name: Example AI
description: Example provider
executable: pinproc-provider-example
settings:
  - key: endpoint
    label: Endpoint
    type: url
    required: true
  - key: api_key
    label: API key
    type: secret
    required: true
  - key: model
    label: Model
    type: string
    required: false
~~~

Supported setting types are string, secret, url, and boolean.

Provider IDs and executable names are intentionally constrained to plain file names.

## Runtime protocol

The provider executable is launched by pinproc for one decision request.

stdin receives one line containing one JSON object:

~~~json
{
  "protocol_version": 1,
  "request_id": "req-123",
  "method": "ask",
  "config": {
    "endpoint": "https://ai.example",
    "api_key": "secret"
  },
  "state": {},
  "questions": {
    "primary_dimension": {
      "type": "choice",
      "instructions": "Which resource is most constrained?",
      "criteria": {
        "cpu": "CPU is scarce",
        "none": "Nothing is constrained"
      }
    }
  }
}
~~~

stdout must contain exactly one JSON object:

~~~json
{
  "protocol_version": 1,
  "request_id": "req-123",
  "answers": {
    "primary_dimension": {
      "type": "choice",
      "choice": "cpu",
      "confidence": 0.91
    }
  }
}
~~~

On failure, the provider can return:

~~~json
{
  "protocol_version": 1,
  "request_id": "req-123",
  "error": "provider-specific failure"
}
~~~

Provider-specific API calls and retries belong inside the provider. pinproc does not retry the AI request because a retry could duplicate an external call.

Provider logs must go to stderr. stdout is reserved for the protocol.

A provider receives the bounded investigation state already prepared by pinproc. It should not assume access to local Linux files, sockets, or credentials beyond the configuration sent in the request.

## Packaging

A provider should be distributed as a separate Debian package that:

1. Depends on pinproc.
2. Installs its executable under /usr/libexec/pinproc/providers/.
3. Installs its manifest under /usr/share/pinproc/providers/.
4. Does not modify /etc/pinproc/config.yaml during installation.
5. Lets the operator select and configure it with sudo pinproc setup ai.

This keeps providers independent from the core package and allows the core to continue operating with deterministic rules if a provider is removed or unavailable.
