# patches (Go)

Go-Version von [dotMatthew/patches](https://github.com/dotMatthew/patches) (Java/JGit/picocli).
Keine Struktur-Kopie, aber dieselbe Ausgabe: gleiche Kommandos, Optionen, Hilfetexte,
Fehlermeldungen, Log-Ausgaben, Exit-Codes, `patches.properties`-Format und byte-identische
Patch-Dateien – abgesichert durch die Black-Box-Testsuite in `e2e/`.

```
patches init | apply | clean | create-patch | config | reset | convert-patches
```

## Aufbau

| Datei / Paket          | Inhalt                                                        |
|------------------------|---------------------------------------------------------------|
| `main.go`, `cli.go`    | Einstieg, Argument-Parser mit picocli-kompatiblen Meldungen   |
| `commands.go`          | die sieben Kommandos                                          |
| `config.go`            | `patches.properties` (java.util.Properties-Format)            |
| `patchfile.go`         | Patch-Datei: Mail-Header (From/Date/Subject), Body, Diff      |
| `git.go`               | clone, reset (clean + hard reset)                             |
| `create.go`, `apply.go`| create-patch, apply, convert-patches                          |
| `log.go`               | Log-Format und Exit-Codes                                     |
| `e2e/`                 | Black-Box-Tests (laufen auch gegen die Java-Version)          |
| `internal/diff`        | Histogram-/Myers-Diff und Diff-Format wie JGit (byte-identische Patches) |
| `internal/patch`       | Patches parsen und anwenden (Hunk-Suche, CRLF) wie JGit       |

Git-Zugriffe laufen über go-git. Reines Go ohne cgo – die Binaries sind statisch und laufen auf amd64 und arm64.

## Installation

Voraussetzung: Go ≥ 1.26.

```
go build -o patches .          # Binary im aktuellen Verzeichnis
go install .                   # nach $GOPATH/bin bzw. $GOBIN
```

Ohne Repo-Checkout (sobald das Repo unter diesem Modulpfad veröffentlicht ist):

```
go install github.com/dotMatthew/patches-go@latest
```

Für andere Plattformen einfach `GOOS`/`GOARCH` setzen, z. B.
`CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o patches .`

## Makefile

| Ziel              | Beschreibung                                                   |
|-------------------|----------------------------------------------------------------|
| `make build`      | statisches Binary `./patches` für die aktuelle Plattform       |
| `make dist`       | `dist/patches-{linux,darwin,windows}-{amd64,arm64}`           |
| `make test`       | `go vet` + Black-Box-Tests (`e2e/`)                            |
| `make test-arm64` | Tests gegen das linux/arm64-Binary unter qemu in Docker        |
| `make clean`      | Build-Artefakte löschen                                        |

## Tests

`go test ./...` baut das Binary selbst und führt die Black-Box-Tests in `e2e/` aus
(benötigt `git` im `PATH`). Dieselben Tests laufen auch gegen jede andere
Implementierung, z. B. die originale Java-Version:

```
PATCHES_CMD="java -jar /pfad/zu/patches.jar" go test -count=1 ./e2e
```

Die Referenzdateien in `e2e/testdata/` stammen aus der Java-Version und können mit
`UPDATE_GOLDEN=1` neu erzeugt werden.

## Bewusste Abweichungen von der Java-Version

* `clean` folgt beim rekursiven Löschen keinen Symlinks (Java/`File.isDirectory` tut das und
  kann so Dateien außerhalb von `_workdir` löschen).
* Nach einem fehlgeschlagenen `apply` bleibt kein `.git/index.lock` liegen (JGit-Bug, der
  danach `reset` scheitern lässt).
* Nicht unterstützt: Rename-Erkennung via `diff.renames`, `GIT binary patch`-Hunks,
  kombinierte/traditionelle Diffs beim Anwenden, `core.autocrlf`/`.gitattributes`-EOL und
  Clean/Smudge-Filter (z. B. LFS).
* Unbehandelte Exceptions (Exit-Code 1) erzeugen eine einzeilige Meldung statt eines Stacktraces;
  Detailtexte von Git-Fehlern stammen von go-git statt JGit.
* Exotische Randfälle werden mit Go-Standardmitteln behandelt statt Java exakt nachzubilden:
  Trimmen nutzt `strings.TrimSpace` (Java `strip`/`trim` unterscheiden sich bei seltenen
  Unicode-/Steuerzeichen), ungültiges UTF-8 wird pro Byte ersetzt, ungültige Kalendertage
  im `Date:`-Header (z. B. 31. Feb.) werden abgelehnt statt aufs Monatsende gerundet.

Patches werden (in beiden Versionen) nach ihrer Nummer am Anfang des Dateinamens sortiert angewendet:
`00001_a.patch` < `002_b.patch` < `9_c.patch` < `10_d.patch`; bei gleicher Nummer entscheidet der Name,
Dateien ohne Nummer kommen zuletzt (alphabetisch). Gilt auch für `convert-patches`.
