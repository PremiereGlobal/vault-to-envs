package main

import (
  "fmt"
  "encoding/json"
  "regexp"
  "strings"
  "time"
  log "github.com/Sirupsen/logrus"
  envconfig "github.com/kelseyhightower/envconfig"
  VaultApi "github.com/hashicorp/vault/api"
  "github.com/aws/aws-sdk-go/aws"
  "github.com/aws/aws-sdk-go/aws/awserr"
  "github.com/aws/aws-sdk-go/aws/session"
  "github.com/aws/aws-sdk-go/aws/credentials"
  "github.com/aws/aws-sdk-go/service/sts"
)

type Specification struct {
    Vault_Addr  string `required:"true"`
    Vault_Token    string `required:"true"`
    Debug         string `default:"false"`
    Secret_Config  string `required:"true"`
}

type SecretConfig struct {
    SecretItems []SecretItem
}

type SecretItem struct {
  SecretPath string
  TTL int
  SecretMaps []SecretMap
}

type SecretMap struct {
  EnvVar string
  VaultKey string
  SecretValue string
}

var VaultClient *VaultApi.Client
var Vault *VaultApi.Logical
var VaultSys *VaultApi.Sys

func main() {

  // General-use error handlers
  var err error
  var ok bool

  // Ensure we have the right ENVs set
  var spec Specification
  err = envconfig.Process("", &spec)
  if err != nil {
      log.Fatal(err.Error())
  }

  // Set log level
  if spec.Debug == "true" {
    log.SetLevel(log.DebugLevel)
    log.Debug("Debug level set")
  } else {
    log.SetLevel(log.InfoLevel)
  }

  // Configure new Vault Client
  conf := &VaultApi.Config{Address: spec.Vault_Addr}
  VaultClient, _ = VaultApi.NewClient(conf)
  VaultClient.SetToken(spec.Vault_Token)

  // Define a Logical Vault client (to read/write values)
  Vault = VaultClient.Logical()
  VaultSys = VaultClient.Sys()

  // This holds all of the secret configuration maps
  secretConfig := SecretConfig{}

  // Unmarshal the SECRET_CONFIG json
  var f interface{}
  err = json.Unmarshal([]byte(spec.Secret_Config), &f)
  if err != nil {
  log.Fatal("Error parsing SECRET_CONFIG: ", err)
  }

  // Iterate over the top level array to get mappings
  jsonItems := f.([]interface{})
  for _, v := range jsonItems {

  // Use type assertions to ensure that the value is a JSON object
  switch jsonItems := v.(type) {

      // The value is an secretItem, represented as a generic interface
      case interface{}:

        // Create a new SecretItem which will hold the details for this secret
        var secretItem SecretItem

        // Access the values in the JSON object and ensure they are of the right structure
        var secretItemArray map[string]interface{}
        secretItemArray, ok = jsonItems.(map[string]interface{})
        if(!ok) {
          log.Fatal("SECRET_CONFIG has invalid structure")
        }

        // Iterate over each key/value within the config item
        for itemKey, itemValue := range secretItemArray {
          switch itemKey {
            case "vault_path":

              // Make sure is a string
              switch itemValue := itemValue.(type) {
                case string:
                  secretItem.SecretPath = itemValue
                default:
                  log.Fatal("SECRET_CONFIG item 'vault_path' should be a string")
              }
            case "ttl":

              // Make sure that TTL is a number; all numbers are transformed to float64
              switch itemValue := itemValue.(type) {
              case float64:
                  secretItem.TTL = int(itemValue)
                default:
                  log.Fatal("SECRET_CONFIG item 'ttl' should be a integer")
              }
            case "set":

              // Make sure that "set" is an interface{} (JSON object)"
              switch setObj := itemValue.(type) {

                case interface{}:

                  // Ensure that the set value has the correct type
                  var setObjArray map[string]interface{}
                  setObjArray, ok = setObj.(map[string]interface{})
                  if(!ok) {
                    log.Fatal("SECRET_CONFIG item 'set' has invalid structure")
                  }

                  // Iterate over each env/key pair and create a SecretMap
                  for envVar, vaultKey := range setObjArray {

                    // Create a new SecretMap to hold our env/key values
                    var secretMap SecretMap

                    // Ensure the the vault key is a string
                    switch vaultKey := vaultKey.(type) {
                      case string:

                        // Ensure env variable passed doesn't have any invalid characters
                        reg, err := regexp.Compile(`^[a-zA-Z_]{1}[a-zA-Z0-9_]*$`)
                        if err != nil {
                          log.Fatal("Error parsing regex", err)
                        }
                        if !reg.MatchString(envVar) {
                          log.Fatal("Invalid environment variable name '", envVar, "'")
                        }

                        // Set our env/key pairs
                        secretMap.EnvVar = envVar
                        secretMap.VaultKey = vaultKey
                        secretItem.SecretMaps = append(secretItem.SecretMaps, secretMap)

                      default:
                        fmt.Println("Incorrect type for '", envVar, "' expected string")
                    }
                  }
                default:
                  log.Fatal("SECRET_CONFIG structure invalid")
              }
            default:
              // Ignore any extra fields that we don't care about
          }
        }
        secretConfig.SecretItems = append(secretConfig.SecretItems, secretItem)

      // Not a JSON object; handle the error
      default:
        log.Fatal("SECRET_CONFIG structure invalid")
    }
  }

  // Retrieve the secrets from Vault
  for _, secretItem := range secretConfig.SecretItems {
    GetSecret(&secretItem)
  }

  // If any of the secrets are AWS keys, wait for them to become active
  mountOutput, err := VaultSys.ListMounts()

  // Loop through secretItems and, if the mount has type aws, wait for AWS credentials to become active
  // TODO: Could probably do this in some sort of multithread manner
  for _, secretItem := range secretConfig.SecretItems {

    // Get the mount for the secret
    mountPath := strings.Split(secretItem.SecretPath, "/")[0]+"/"

    // Loop through Vault mounts to get type
    for mountKey, mountData := range mountOutput {
      if mountKey == mountPath && mountData.Type == "aws" {

        // Wait for the credentials to become active by making an aws get-caller-identity call
        waitForAwsCredsToActivate(&secretItem)
      }
    }
  }

  // No errors, output the results
  DisplaySecretEnvs(&secretConfig)

  // TODO: Zero out the secret from memory
  // TODO: Revoke dynamic secrets on failure

}

func GetSecret(secretItem *SecretItem) {

  // Read the secret from Vault
  log.Info("Fetching secret: ", secretItem.SecretPath)
  secret, err := Vault.Read(secretItem.SecretPath)
  if err != nil {
    log.Fatal("Error fetching secret: ", err.Error())
  }

  // If we got back an empty response, fail
  if secret == nil {
    log.Fatal("Could not find secret ", secretItem.SecretPath)
  }

  // Ensure that secret is renewable if trying to set the TTL
  if secretItem.TTL != 0 && !secret.Renewable  {
    log.Fatal("Cannot set TTL on secret ", secretItem.SecretPath, ". TTL can only be set on dynamic secrets like AWS credentials")
  } else if secretItem.TTL != 0 {
    log.Info("Renewing lease on ", secretItem.SecretPath, " to ", secretItem.TTL, " seconds")
    log.Info("Original Lease Info ", secret.LeaseID, ",", secret.LeaseDuration)
    renewed_secret, err := VaultSys.Renew(secret.LeaseID, secretItem.TTL)
    if err != nil {
      log.Fatal("Error renewing secret (setting TTL): ", err.Error())
    }
    log.Info("New Lease Info ", renewed_secret.LeaseID, ",", renewed_secret.LeaseDuration)

    // Check if lease duration was able to be set to desired amount
    // Added some tolerance for any request delay
    if (secretItem.TTL-renewed_secret.LeaseDuration) > 5 {
      log.Fatal("Not able to set TTL to desired amount. Desired:", secretItem.TTL, "; Actual:", renewed_secret.LeaseDuration)
    }
  }

  for key, secretMap := range secretItem.SecretMaps {

    if secret.Data[secretMap.VaultKey] == nil {
      log.Fatal("Key '", secretMap.VaultKey, "' not found in secret '", secretItem.SecretPath, "'")
    }

    secretItem.SecretMaps[key].SecretValue = secret.Data[secretMap.VaultKey].(string)
  }
}

func DisplaySecretEnvs(secretConfig *SecretConfig) {
  // Display Environment variable output
  for _, secretItem := range secretConfig.SecretItems {
    for _, secretMap := range secretItem.SecretMaps {

      // Prints the env variable line to stdout
      // Single quotes value and escapes single quotes in secret with '"'"'
      fmt.Println("export "+secretMap.EnvVar+"='"+strings.Replace(secretMap.SecretValue, "'", "'\"'\"'", -1)+"'")
    }
  }
}

func waitForAwsCredsToActivate(secretItem *SecretItem) {

  // Retrieve ID/Key from secretItem
  var accessKey string
  var secretKey string
  for _, v := range secretItem.SecretMaps {
    if v.VaultKey == "access_key" {
      accessKey = v.SecretValue
    } else if v.VaultKey == "secret_key" {
      secretKey = v.SecretValue
    }
  }

  // Ensure both are set (if not the user didn't set them and we should error out)
  // TODO: make this happen before requesting the credentials
  if accessKey == "" {
    log.Fatal("Vault key 'access_key' for AWS credential provider ", secretItem.SecretPath, " not assigned to ENV var")

  }
  if secretKey == "" {
    log.Fatal("Vault key 'secret_key' for AWS credential provider ", secretItem.SecretPath, " not assigned to ENV var")
  }

  awsCreds := credentials.NewStaticCredentials(accessKey, secretKey, "")
  sess, err := session.NewSession(&aws.Config{
        Credentials: awsCreds},
    )
  if err != nil {
    log.Fatal("Error creating AWS session: ", err)
  }

  // Create a IAM service client.
  svc := sts.New(sess)

  // Try to get caller identity until it becomes active
  err = retry(20, time.Second, func() error {

    _, err := svc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
    if awserr, ok := err.(awserr.Error); ok {
      if awserr.Code() == "InvalidClientTokenId" {
        log.Info("AWS credentials not yet active, waiting...")
        return err
      } else {
        log.Fatal("Error validating AWS credentials: ", err)
      }
    }

    log.Info("AWS credentials (", accessKey,") from ", secretItem.SecretPath," active")
		return nil
	})

  if err != nil {
    log.Fatal("Error validating AWS credentials (not active within set duration) ", err)
  }

}

func retry(attempts int, sleep time.Duration, fn func() error) error {
	if err := fn(); err != nil {
		if s, ok := err.(stop); ok {
			// Return the original error for later checking
			return s.error
		}

		if attempts--; attempts > 0 {
			time.Sleep(sleep)
			return retry(attempts, 2*sleep, fn)
		}
		return err
	}
	return nil
}

type stop struct {
	error
}
