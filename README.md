## modtransplant

We recently went through the process of merging a project dependency into the
main project (removing the dependency) at [InfluxData](https://influxdata.com).
After struggling to do the `go.mod` merging by hand, I wrote this tool to help
with the process.The goal is to keep changes the resulting `go.mod` (after
triggering the `go` tool) to a minimum, and yield a depeendecy graph that's as
close as possible to the original.

### Usage

```
$ go install github.com/brettbuddin/modtransplant
$ modtransplant -dest=project-a/go.mod -src=project-b/go.mod [-force-overwrite] > go-merged.mod
```

The `-dest` is a filepath to the `go.mod` file of your destination module.

The `-src` is a filepath to the `go.mod` file of the module you are merging into
the destination module.

The optional `-force-overwrite` flag forces overwriting of a module path's
version in the destination when the tool detects two versions that it cannot
compare (e.g. `v0.5.0` vs `v0.0.0-20190523213315-cbe66965904d`). Ideally this
shouldn't be necessary, but if you have a project with enough old dependencies,
it might be useful.
