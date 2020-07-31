package main

import (
  "fmt"
  "os"
  "strings"

  "encoding/json"
  "net/http"

  admV1b1 "k8s.io/api/admission/v1beta1"
  appsV1 "k8s.io/api/apps/v1"
  coreV1 "k8s.io/api/core/v1"
  metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

  "github.com/gin-gonic/gin"
  cApi "github.com/hashicorp/consul/api"
  log "github.com/inconshreveable/log15"
)

type Patch struct {
  Op  string        `json:"op"`
  Path string       `json:"path"`
  Value interface{} `json:"value,omitempty"`
}

// mutate injects data from Consul as environment variables into deployments.
// It uses environment variable CONSUL_SITE to generate a Consul prefix of
// "common/site/CONSUL_SITE/". Files in that prefix will be injected
// as environment variables into deployments that has "releng.diligent.com/use-consul"
// annotation set to "true".
func mutate(c *gin.Context) {
  // ar contains the AdmissionReview object sent by Kubernetes
  var ar *admV1b1.AdmissionReview
  c.BindJSON(&ar)

  // deploy contains the Deployment object unmarshalled from AdmissionReview object
  var deploy *appsV1.Deployment
  json.Unmarshal(ar.Request.Object.Raw, &deploy)

  // Only mutate deployment if annotation exist and set to "true"
  var resp *admV1b1.AdmissionResponse
  rAnno := "releng.diligent.com/use-consul"
  if anno, ok := deploy.Annotations[rAnno]; ok && strings.ToLower(anno) == "true" {
    message := fmt.Sprintf("Mutating deployment %s in %s namespace with Consul data", deploy.Name, deploy.Namespace)
    log.Info(message)

    cEvars, err := getConsulData()
    if err != nil {
      log.Crit("Unable to fetch data from Consul")
    }

    patches := createPatch(deploy.Spec.Template.Spec.Containers, cEvars)
    resp := createResp(patches, message)
    resp.UID = ar.Request.UID
  } else {
    message := fmt.Sprintf("Not mutating deployment %s in %s namespace, missing annotation", deploy.Name, deploy.Namespace)
    log.Warn(message)
    resp := createResp([]Patch{}, message)
    resp.UID = ar.Request.UID
  }
  ar.Response = resp
  respBody, _ := json.Marshal(ar)
  c.JSON(http.StatusOK, respBody)
}

func createResp(patches []Patch, message string) *admV1b1.AdmissionResponse {
  resp := admV1b1.AdmissionResponse{}
  resp.Allowed = true
  resp.Result = &metaV1.Status{
    Message: message,
  }

  if len(patches) > 0 {
    patchBytes, _ := json.Marshal(patches)
    resp.Patch = patchBytes
    pType := admV1b1.PatchTypeJSONPatch
    resp.PatchType = &pType
  }

  return &resp
}

// getConsulData get Consul data as environment variables for a site.
func getConsulData() ([]coreV1.EnvVar, error) {
  var cEvars []coreV1.EnvVar

  // Get env vars in Consul at `common/site/{SITE}/` prefix
  prefix := fmt.Sprintf("common/site/%s/", os.Getenv("CONSUL_SITE"))
  consul, _ := cApi.NewClient(cApi.DefaultConfig())
  cKVPairs, _, err := consul.KV().List(prefix, &cApi.QueryOptions{})
  if err != nil {
    return cEvars, err
  }

  for _, kvPair := range cKVPairs {
    key := strings.Replace(string(kvPair.Key), prefix, "", 1)
    eVar := coreV1.EnvVar{Name: key, Value: string(kvPair.Value)}
    cEvars = append(cEvars, eVar)
  }
  return cEvars, nil
}

// createPatch generates a JSONPatch object for Kubernetes to mutate the deployment.
func createPatch(containers []coreV1.Container, cEvars []coreV1.EnvVar) []Patch {
  patches := []Patch{}
  for i, c := range containers {
    conEvars := getConEvars(c.Env, cEvars)
    path := fmt.Sprintf("/spec/template/spec/containers/%d/env", i)
    patch := Patch{Op: "append", Path: path, Value: conEvars }
    patches = append(patches, patch)
    /*
    for j, e := range conEvars {
      //fmt.Printf("c: %d, v: %d, name: %s, value: %s\n", i, j, e.Name, e.Value)
      namePath := fmt.Sprintf("/spec/template/spec/containers/%d/env/%d/name", i, j)
      valuePath := fmt.Sprintf("/spec/template/spec/containers/%d/env/%d/value", i, j)
      patches = append(patches, Patch{"op": "replace", "path": namePath, "value": e.Name})
      patches = append(patches, Patch{"op": "replace", "path": valuePath, "value": e.Value})
    }
    */
  }
  return patches
}

// getConEvars merges container's env vars with variables from Consul. If same variable
// exists in both, Consul's variable takes precedence.
func getConEvars(conEvars []coreV1.EnvVar, cEvars []coreV1.EnvVar) []coreV1.EnvVar {
  var eVars []coreV1.EnvVar

  // Only take evars that do not exist in Consul's evars list
  for _, v := range conEvars {
    if !hasEvar(cEvars, v) {
      eVars = append(eVars, v)
    }
  }

  // Append rest of Consul's evars
  for _, v := range cEvars {
    eVars = append(eVars, v)
  }

  return eVars
}

// hasEvars returns whether an evar is in an evars list.
func hasEvar(cEvars []coreV1.EnvVar, v coreV1.EnvVar) bool {
  for _, cv := range cEvars {
    if cv.Name == v.Name {
      return true
    }
  }
  return false
}

