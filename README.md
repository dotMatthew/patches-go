# patches (Go)

Go-Version von [dotMatthew/patches](https://github.com/dotMatthew/patches) (Java/JGit/picocli).
Keine Struktur-Kopie, aber dieselbe Ausgabe: gleiche Kommandos, Optionen, Hilfetexte,
Fehlermeldungen, Log-Ausgaben, Exit-Codes und byte-identische Patch-Dateien – abgesichert
durch die Black-Box-Testsuite in `e2e/`. Einzige gewollte Abweichung im Verhalten: die
Konfiguration ist TOML statt `.properties` (siehe [Konfiguration](#konfiguration)).

```
patches init | apply | clean | create-patch | config | reset | convert-patches
```

## Aufbau

| Datei / Paket           | Inhalt                                                            |
|-------------------------|-------------------------------------------------------------------|
| `main.go`               | Einstieg: Signal-Handling (Ctrl+C), Exit-Code                     |
| `app.go`                | `app` (Verzeichnis, Ein-/Ausgaben, Context), Logging, `cmdError`  |
| `commands.go`, `usage/` | die sieben Kommandos und ihre Hilfetexte (per `go:embed`)         |
| `config.go`             | `patches.toml` lesen/schreiben, Migration von `.properties`       |
| `properties.go`         | Lesen des alten `patches.properties`-Formats (nur für die Migration) |
| `patchfile.go`          | Patch-Datei: Mail-Header (From/Date/Subject), Body, Diff          |
| `git.go`                | clone, reset (clean + hard reset)                                 |
| `create.go`, `apply.go` | create-patch, apply, convert-patches                              |
| `version.go`            | Versionsermittlung                                                |
| `internal/cli`          | Argument-Parser mit picocli-kompatiblen Meldungen                 |
| `internal/diff`         | Histogram-/Myers-Diff und Diff-Format wie JGit (byte-identische Patches) |
| `internal/patch`        | Patches parsen und anwenden (Hunk-Suche, CRLF) wie JGit           |
| `e2e/`                  | Black-Box-Tests (laufen auch gegen die Java-Version)              |

Kommandos geben einen `error` zurück; ein `cmdError` bestimmt Exit-Code und Log-Meldung,
ausgegeben wird an genau einer Stelle (`app.report`). Alle Pfade sind relativ zu `app.dir`,
dadurch lassen sich die Kommandos auch im Prozess testen.

Git-Zugriffe laufen über go-git. Reines Go ohne cgo – die Binaries sind statisch und laufen
auf amd64 und arm64.

## Konfiguration

Die Konfiguration liegt in `patches.toml` (andere Datei mit `-F`):

```toml
# patches configuration
base_repo_url = "https://github.com/example/repo.git"
base_repo_ref = "main"            # Branch, Tag, Ref-Name oder Commit-SHA
patches_directory = "patches"     # optional, Standard: patches
```

`patches init` legt die Datei an (bei `-F` an diesem Ort, bei `-F name.properties` als
`name.toml`), `patches config` ändert sie (die Datei wird dabei neu
geschrieben, eigene Kommentare gehen verloren). Unbekannte Schlüssel sind ein Fehler
(Tippfehlerschutz).

**Migration von `patches.properties`:** Die Java-Version (und Go-Versionen < 2.0.0) nutzen
`patches.properties`. Dieses Format wird nur noch gelesen: Gibt es keine `patches.toml`, aber
eine `patches.properties`, migriert jedes Kommando sie automatisch – es schreibt
`patches.toml` und benennt die alte Datei in `patches.properties.bak` um. Mit
`-F name.properties` wird entsprechend nach `name.toml` migriert, danach wird die TOML-Datei
verwendet. Eine ungültige `.properties`-Datei wird nicht migriert; es erscheinen dieselben
Fehlermeldungen wie bisher.

| `.properties`          | `.toml`             |
|------------------------|---------------------|
| `baseRepoUrl`          | `base_repo_url`     |
| `baseRepoRef`          | `base_repo_ref`     |
| `patchesDirectoryPath` | `patches_directory` |

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

`patches -V` gibt die Version aus dem Git-Tag aus (`v1.2.3` → `patches 1.2.3`); bei
ungetaggten Commits eine Pseudo-Version, bei lokalen Änderungen mit `+dirty`, ohne
Git-Informationen `dev`. Mit `-ldflags "-X main.version=1.2.3"` (bzw. `make build VERSION=1.2.3`)
lässt sie sich explizit setzen.

Für andere Plattformen einfach `GOOS`/`GOARCH` setzen, z. B.
`CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o patches .`

## Makefile

| Ziel              | Beschreibung                                                   |
|-------------------|----------------------------------------------------------------|
| `make build`      | statisches Binary `./patches` für die aktuelle Plattform       |
| `make dist`       | `dist/patches-{linux,darwin,windows}-{amd64,arm64}`           |
| `make checksums`  | `dist/` plus `dist/checksums.txt` (SHA-256)                    |
| `make test`       | `go vet`, Unit- und Black-Box-Tests (`e2e/`)                   |
| `make fuzz`       | Fuzzing von Diff und Diff→Apply-Roundtrip (`FUZZTIME=1m`)      |
| `make lint`       | `golangci-lint run` (Konfiguration in `.golangci.yml`)         |
| `make test-arm64` | Tests gegen das linux/arm64-Binary unter qemu in Docker        |
| `make clean`      | Build-Artefakte löschen                                        |

## Tests

`go test ./...` führt die Unit-Tests und die Black-Box-Tests in `e2e/` aus; für letztere
wird das Binary automatisch gebaut (benötigt `git` im `PATH`). Die Fuzz-Tests prüfen, dass
ein erzeugter Diff angewendet immer exakt den Zieltext ergibt (`make fuzz`). Dieselben Tests laufen auch gegen jede andere
Implementierung, z. B. die originale Java-Version:

```
PATCHES_CMD="java -jar /pfad/zu/patches.jar" PATCHES_VERSION=1.0.0 PATCHES_JAVA=1 go test -count=1 ./e2e
```

`PATCHES_VERSION` ist die Version, die `-V` ausgeben muss; ohne die Variable wird nur das
Format geprüft. Das von den Tests selbst gebaute Binary bekommt die Version `0.0.0-e2e`.
`PATCHES_JAVA=1` kennzeichnet die Java-Version: Die Tests schreiben dann `patches.properties`
statt `patches.toml`, und die Tests für das TOML-Format und die Migration (markiert mit
`goOnly`) werden übersprungen.

Die Referenzdateien in `e2e/testdata/` stammen aus der Java-Version und können mit
`UPDATE_GOLDEN=1` neu erzeugt werden.

## Releases

Releases entstehen automatisch mit [semantic-release](https://semantic-release.gitbook.io)
(`.releaserc.yml`), sobald auf `main` gepusht wird und die Tests grün sind. Aus den
Conventional Commits seit dem letzten Tag ergibt sich die Version (`fix:` → Patch,
`feat:` → Minor, `BREAKING CHANGE` → Major); das erste Release ist `v1.0.0`. Es wird der
Tag `vX.Y.Z` und ein GitHub-Release mit Release-Notes, allen Binaries aus `make dist`
und `checksums.txt` erstellt. Es wird nichts ins Repo zurück committet, ein zusätzliches
Token ist nicht nötig. Commits wie `docs:`, `test:`, `ci:` oder `build:` lösen kein Release aus.

## Bewusste Abweichungen von der Java-Version

* **Konfigurationsformat (Breaking Change ab 2.0.0):** `patches.toml` mit den Schlüsseln
  `base_repo_url`, `base_repo_ref` und `patches_directory` statt `patches.properties`.
  `patches.properties` wird nur noch gelesen und automatisch migriert (siehe
  [Konfiguration](#konfiguration)). Die Hilfetexte nennen entsprechend `patches.toml`, und
  Meldungen zur TOML-Datei sprechen von „config file“ statt „properties file“.
* `init -F datei` legt die Beispiel-Konfiguration in der angegebenen Datei an; die
  Java-Version schreibt sie unabhängig von `-F` immer nach `patches.properties`.
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
