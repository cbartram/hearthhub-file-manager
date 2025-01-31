# HearthHub File Manager
A kubernetes `Job` which downloads and installs & uninstalls Valheim plugins, world files, and configuration to/from a shared volume for 
the dedicated server to load and use.

This job has 3 parts:

- Scale down existing replicas of the server
- Pull the necessary file(s) from S3
- Write/Remove the files from a given directory on the PVC.

## Arguments

The file manager takes the following arguments:

| Arg Name        | Arg Type | Description                                                                                                                                 | Example Usage                             |
|-----------------|----------|---------------------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------|
| `discord_id`    | `string` | The users discord ID                                                                                                                        | `-discord_id "123456789012345678"`        |
| `refresh_token` | `string` | The users refresh token                                                                                                                     | `-refresh_token "abc123xyz456"`           |
| `prefix`        | `string` | S3 prefix name including the extension. Example: `file.zip`                                                                                 | `-prefix "/mods/general/ValheimPlus.zip"` |
| `destination`   | `string` | PVC volume destination. This path does NOT need to include the file name as it will be parsed from the prefix automatically.                | `-destination "/valheim/BepInEx/plugins"` |
| `archive`       | `string` | If the file being downloaded is an archive and needs unpacked. For delete op's the archive will be used to determine which files to remove. | `-archive "true"`                         |
| `op`            | `string` | Operation to perform, either `"write"` or `"delete"`                                                                                        | `-op "write"`                             |

All arguments are required.

## Building

You can build the application locally using: `go build -o main .` and run with `./main -discord_id "foo" -refresh_token "bar" ...`. 
This is designed to function as a Kubernetes `Job`. See [HearthHub Kube API's](https://github.com/cbartram/hearthhub-kube-api) file_handler route
for an example of the `Job` manifest.

## Docker Build

To build with docker run the following replacing the `0.0.1` tag with your desired tag:

```shell
./build.sh 0.0.1
```

## Testing

Run unit tests for this software with:

```shell
go test ./... -v
```

To generate coverage reports run:

```shell
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Built With

- [Kubernetes](https://kubernetes.io) - Container orchestration platform
- [Helm](https://helm.sh) - Manages Kubernetes deployments
- [Docker](https://docker.io/) - Container build tool

## Contributing

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on our code
of conduct, and the process for submitting pull requests to us.

## Versioning

We use [Semantic Versioning](http://semver.org/) for versioning. For the versions
available, see the [tags on this
repository](https://github.com/cbartran/hearthhub-mod-api/tags).

## Authors

- **cbartram** - *Initial work* - [cbartram](https://github.com/cbartram)

## License

This project is licensed under the [CC0 1.0 Universal](LICENSE)
Creative Commons License - see the [LICENSE.md](LICENSE) file for
details