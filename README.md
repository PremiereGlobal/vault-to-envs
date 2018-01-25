# Extracting Vault Secrets into Environment Variables

A Docker container for extracting Vault secrets into environment variables for use in deploys or development.

<!-- TOC depthFrom:2 depthTo:6 withLinks:1 updateOnSave:1 orderedList:0 -->

- [Prerequisites](#prerequisites)
- [Basic Usage](#basic-usage)
- [Docker Environment Variables](#docker-environment-variables)
- [Configuration](#configuration)
	- [Examples](#examples)
		- [Simple Secrets](#simple-secrets)
		- [Dynamic Secrets](#dynamic-secrets)
- [Sourcing the Env Vars](#sourcing-the-env-vars)

<!-- /TOC -->

## Prerequisites

* A Vault instance
 * A Valid Authentication Token

## Basic Usage

```bash
docker run \
	--rm \
	-e VAULT_ADDR="https://vault.my-domain.com:8200" \
	-e VAULT_TOKEN="<token>" \
	-e SECRET_CONFIG="<configuration (see below)>"
	readytalk/vault-to-envs:latest
```

Will output, as an example:

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
|`SECRET_CONFIG`| Definition of which secrets/keys to extract and what environment variables to set them to. See below for more details. | required |
|`DEBUG`| Set to `true` to output verbose details during execution | `false` |

## Configuration
This container is configured with the `SECRET_CONFIG` environment variable which is a JSON formatted set of settings that determine which secrets get extracted from Vault.

### Examples

#### Simple Secrets
Take an example where we have two secrets.  The first contains 3 keys with database information.  The second contains some type of token.

`secret_config.json`
```json
[
	{
		"vaultPath": "secret/app/database",
		"set": {
			"DB_HOST": "dbHost",
			"DB_USER": "dbUser",
			"DB_PASSWORD": "dbPass"
		}
	},
	{
		"vaultPath": "secret/app/token",
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
	-e VAULT_ADDR="https://vault.my-domain.com:8200" \
	-e VAULT_TOKEN="<token>" \
	-e SECRET_CONFIG="$(cat secret_config.json)"
	readytalk/vault-to-envs:latest
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
		"vaultPath": "aws/creds/my-role",
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
	-e SECRET_CONFIG="$(cat secret_config.json)"
	readytalk/vault-to-envs:latest
```

Output
```
export AWS_SECRET_ACCESS_KEY='xxxxxxxxxxxxxxxxxxxxxxxxx'
export AWS_ACCESS_KEY_ID='xxxxxxxxxxxxxxxxxx'
```

## Sourcing the Env Vars
One way to source the output of the container is to simply eval the `docker run` output. If a successful run occurs the stdout will be evaluated and the environment variables set.

```
eval $(docker run \
	--rm \
	-e VAULT_ADDR="https://vault.my-domain.com:8200" \
	-e VAULT_TOKEN="<token>" \
	-e SECRET_CONFIG="<configuration (see below)>"
	readytalk/vault-to-envs)"
```
