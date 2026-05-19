group "default" {
  targets = ["basement"]
}

target "basement" {
  context    = "."
  dockerfile = "docker/Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags       = ["ghcr.io/mattjackson/basement:dev"]
  output     = ["type=image,push=false"]
}
