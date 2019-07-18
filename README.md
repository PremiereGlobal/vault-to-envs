# Vault to Env [![Build Status](https://travis-ci.org/PremiereGlobal/vault-to-envs.svg?branch=master)](https://travis-ci.org/PremiereGlobal/vault-to-envs) [![GoDoc](https://godoc.org/github.com/PremiereGlobal/vault-to-envs?status.png)](https://godoc.org/github.com/PremiereGlobal/vault-to-envs/pkg/vaulttoenvs) [![Go Report Card](https://goreportcard.com/badge/github.com/PremiereGlobal/vault-to-envs)](https://goreportcard.com/report/github.com/PremiereGlobal/vault-to-envs) [![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/PremiereGlobal/vault-to-envs/blob/master/LICENSE) 

https://travis-ci.org/PremiereGlobal/vault-to-envs.svg?branch=master

A Docker container for extracting Vault secrets into environment variables for use in deploys or development.

## Prerequisites

* A Vault instance
* A Valid Authentication Token

## Basic Usage

```bash
docker run \
  --rm \
  -e VAULT_ADDR="https://vault.my-domain.com:8200" \
  -e VAULT_TOKEN="<token>" \
  -e SECRET_CONFIG_FILE="./secrets.json"
  premiereglobal/vault-to-envs:latest
```

Output:

```bash
export DB_PASSWORD=abc123
export AWS_ACCESS_KEY_ID=abc123
export AWS_SECRET_KEY=abc123
```

## Docker Environment Variables

To customize some properties of the container, the following environment
variables can be passed via the `-e` parameter (one for each variable).  Value
of this parameter has the format `<VARIABLE_NAME>=<VALUE>`.

| Variable       | Description                                  | Default/Required |
|----------------|----------------------------------------------|---------|
|`VAULT_ADDR`| The full address of the instance of vault to connect to. For example `https://vault.my-domain.com:8200` | required |
|`VAULT_TOKEN`| Vault token to use for authentication. | required |
|`SECRET_CONFIG`| Definition of which secrets/keys to extract and what environment variables to set them to. See below for more details. | required if `SECRET_CONFIG_FILE` not set |
|`SECRET_CONFIG_FILE`| Location of a secret config file. | required if `SECRET_CONFIG` not set |
|`DEBUG`| Set to `true` to output verbose details during execution | `false` |

## Configuration
This container is configured with a JSON formatted string or file (`SECRET_CONFIG` or `SECRET_CONFIG_FILE`) which describes the secrets, env variables, ttl and versions to extract.

### Examples

#### Key-Value Secrets
Take an example where we have two secrets.  The first contains 3 keys with database information.  The second contains some type of token.

`secret_config.json`
```json
[
  {
    "vault_path": "secret/app/database",
    "set": {
      "DB_HOST": "dbHost",
      "DB_USER": "dbUser",
      "DB_PASSWORD": "dbPass"
    }
  },
  {
    "vault_path": "secret/app/token",
    "set":  {
      "APP_TOKEN": "token"
    }
  }
]
```

Command
```bash
docker run \
  --rm \
  -v $(pwd):/config \
  -e VAULT_ADDR="https://vault.my-domain.com:8200" \
  -e VAULT_TOKEN="<token>" \
  -e SECRET_CONFIG_FILE=/config/secret_config.json \
  premiereglobal/vault-to-envs:latest
```

Output
```
export DB_HOST='xxxxxxxxxxxxxx'
export DB_USER='xxxxxx'
export DB_PASSWORD='xxxxxxxxxxxxxxx'
export APP_TOKEN='xxxxxxxxxxxxxxxxx'
```

#### Dynamic Secrets
This example uses [Vault's AWS Secret Backend](https://www.vaultproject.io/docs/secrets/aws/) to create an access/secret key for an AWS account.  The only difference in this example is that we can set a TTL that will try to be met, if allowed. If no TTL is set, the lease duration will be whatever default is configured within Vault.

`secret_config.json`
```json
[
  {
    "vault_path": "aws/creds/my-role",
    "ttl": 600,
    "set": {
      "AWS_ACCESS_KEY_ID": "access_key",
      "AWS_SECRET_ACCESS_KEY": "secret_key"
    }
  }
]
```

Command
```bash
docker run \
  --rm \
  -e VAULT_ADDR="https://vault.my-domain.com:8200" \
  -e VAULT_TOKEN="<token>" \
  -e SECRET_CONFIG="$(cat secret_config.json)" \
  premiereglobal/vault-to-envs:latest
```

Output
```
export AWS_ACCESS_KEY_ID='xxxxxxxxxxxxxxxxxx'
export AWS_SECRET_ACCESS_KEY='xxxxxxxxxxxxxxxxxxxxxxxxx'
```

#### Key-Value (Version 2) Secrets
This example pulls secrets from [Vault's KV V2](https://www.vaultproject.io/docs/secrets/kv/kv-v2.html) data store.  With kv-v2, an additional option for version can be specified.

`secret_config.json`
```json
[
  {
    "vault_path": "kv/app/database",
    "version": 5,
    "set": {
      "DB_HOST": "dbHost",
      "DB_USER": "dbUser",
      "DB_PASSWORD": "dbPass"
    }
  }
]
```

The config above will pull version 5 of the secret specified.

Additionally, a negative value can be specified for version to "go back" a number of version.  For example:

`secret_config.json`
```json
[
  {
    "vault_path": "kv/app/database",
    "version": -2,
    "set": {
      "DB_HOST": "dbHost",
      "DB_USER": "dbUser",
      "DB_PASSWORD": "dbPass"
    }
  }
]
```

This will pull the secrets 2 version behind the current version. Note: any deleted version will be skipped over and the next non-deleted secret will be considered.

## Sourcing the Env Vars
One way to source the output of the container is to simply eval the `docker run` output. If a successful run occurs the stdout will be evaluated and the environment variables set.

```
eval $(docker run \
  --rm \
  -v $(pwd):/config \
  -e VAULT_ADDR="https://vault.my-domain.com:8200" \
  -e VAULT_TOKEN="<token>" \
  -e SECRET_CONFIG_FILE=/config/secret_config.json \
  premiereglobal/vault-to-envs)"
```
