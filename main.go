package main

import (
  "os"
  "net/http"

  "github.com/gin-gonic/gin"
  cApi "github.com/hashicorp/consul/api"
  log "github.com/inconshreveable/log15"
)

func main() {
  r := gin.Default()
  r.GET("/", health)
  r.POST("/mutate", mutate)
  r.Run()
}

// init validates that CONSUL_HTTP_ADDR and CONSUL_SITE
// env vars are defined.
func init() {
  if _, exists := os.LookupEnv("CONSUL_HTTP_ADDR"); exists != true {
    log.Crit("Environment variable CONSUL_HTTP_ADDR not defined")
    os.Exit(1)
  }

  if _, exists := os.LookupEnv("CONSUL_SITE"); exists != true {
    log.Crit("Environment variable CONSUL_SITE not defined")
    os.Exit(1)
  }
}

// health returns connection status with Consul's leader.
func health(c *gin.Context) {
  consul, _ := cApi.NewClient(cApi.DefaultConfig())
  if l, err := consul.Status().Leader(); err == nil {
    c.JSON(http.StatusOK, gin.H{"status": "healthy", "leader": l})
  } else {
    c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
  }
}

