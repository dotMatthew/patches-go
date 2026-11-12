# patches (Go)

Go-Version von [dotMatthew/patches](https://github.com/dotMatthew/patches) (Java/JGit/picocli).
Kommandos, Optionen und Log-Format entsprechen dem Original, die erzeugten Patch-Dateien sind
byte-identisch. Seit 2.0.0 ist die Go-Version aber eigenständig: Konfiguration in TOML,
einheitliche Exit-Codes und klare Fehlermeldungen statt der Eigenheiten der Java-Version
(siehe [Unterschiede zur Java-Version](#unterschiede-zur-java-version)).

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
| `e2e/`                  | Black-Box-Tests des fertigen Binaries                             |

Kommandos geben einen `error` zurück; ein `cmdError` bestimmt Exit-Code und Log-Meldung,
ausgegeben wird an genau einer Stelle (`app.report`). Alle Pfade sind relativ zu `app.dir`,
dadurch lassen sich die Kommandos auch im Prozess testen.

Git-Zugriffe laufen über go-git. Reines Go ohne cgo – die Binaries sind statisch und laufen
auf amd64 und arm64.

## Benutzung

```
patches init [-F <config>] [-U <url>]   # Config anlegen und Base-Repo nach _workdir klonen
patches apply                           # alle Patches als Commits auf _workdir anwenden
patches create-patch [-n <name>]        # HEAD-Commit von _workdir als Patch speichern
patches reset                           # _workdir auf base_repo_ref zurücksetzen
patches config --base-repo-ref <ref>    # Config ändern (auch --base-repo-url, --patches-directory-path)
patches convert-patches                 # Patches als reine git-Diffs nach ./converted
patches clean                           # _workdir löschen
```

`create-patch` fragt den Namen auf der Standardeingabe ab, wenn er nicht mit `-n`/`--name`
angegeben ist. Patches werden nach der Nummer am Anfang des Dateinamens sortiert angewendet:
`00001_a.patch` < `002_b.patch` < `9_c.patch` < `10_d.patch`; bei gleicher Nummer entscheidet
der Name, Dateien ohne Nummer kommen zuletzt (alphabetisch).

| Exit-Code | Bedeutung                                                                   |
|-----------|-----------------------------------------------------------------------------|
| 0         | Erfolg                                                                      |
| 1         | Datei-/Systemfehler, unerwartete Fehler                                     |
| 2         | Falscher Aufruf (unbekannte Option, fehlender Wert, …)                      |
| 8         | Bedienfehler: Config fehlt/ungültig, ungültiger Patch, Repo nicht sauber, …  |
| 42        | Git-Fehler (Klonen, Anwenden, Zurücksetzen)                                  |

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
ein erzeugter Diff angewendet immer exakt den Zieltext ergibt (`make fuzz`).

Mit `PATCHES_CMD` laufen die Black-Box-Tests gegen ein fertiges Binary, z. B. für eine
andere Architektur; `PATCHES_VERSION` ist dann die Version, die `-V` ausgeben muss (ohne
die Variable wird nur das Format geprüft):

```
PATCHES_CMD="qemu-aarch64-static $PWD/dist/patches-linux-arm64" go test -count=1 ./e2e
```

Die Referenz-Patches in `e2e/testdata/` wurden mit der Java-Version erzeugt und sichern ab,
dass die Patch-Dateien byte-identisch bleiben; `UPDATE_GOLDEN=1` schreibt sie neu.

## Releases

Releases entstehen automatisch mit [semantic-release](https://semantic-release.gitbook.io)
(`.releaserc.yml`), sobald auf `main` gepusht wird und die Tests grün sind. Aus den
Conventional Commits seit dem letzten Tag ergibt sich die Version (`fix:` → Patch,
`feat:` → Minor, `BREAKING CHANGE` → Major); das erste Release ist `v1.0.0`. Es wird der
Tag `vX.Y.Z` und ein GitHub-Release mit Release-Notes, allen Binaries aus `make dist`
und `checksums.txt` erstellt. Es wird nichts ins Repo zurück committet, ein zusätzliches
Token ist nicht nötig. Commits wie `docs:`, `test:`, `ci:` oder `build:` lösen kein Release aus.

## Unterschiede zur Java-Version

Ab 2.0.0 (Breaking Change):

* **Konfiguration:** `patches.toml` statt `patches.properties` (siehe
  [Konfiguration](#konfiguration)); alte Dateien werden automatisch migriert.
  `init -F datei` legt die Beispiel-Konfiguration in der angegebenen Datei an.
* **Exit-Codes:** einheitlich (siehe [Benutzung](#benutzung)). In Java war z. B. eine
  fehlende Config je nach Kommando Code 8 oder 42, und Fehler wie „Patch-Datei nicht
  schreibbar“ endeten trotzdem mit 0.
* **Fehler statt Eigenheiten:** Eine ungültige Config bricht jedes Kommando mit einer
  Fehlermeldung ab (Java: teils Absturz). `create-patch` ohne Namen bricht ab (Java:
  `null.patch`), ebenso Patches ohne `From`, `Date` oder `Subject` – und zwar bevor
  etwas angewendet wird (Java: Betreff `null` bzw. halb angewendeter Patch).
  `init` auf ein vorhandenes `_workdir` und `create-patch` auf den ersten Commit melden
  einen Fehler statt abzustürzen; Commit-Nachrichten ohne Zeilenumbruch funktionieren.
* **`config --patches-directory-path`:** Existiert das Ziel bereits, wird nichts geändert
  (Java: Config geändert, Patches blieben im alten Verzeichnis). Existiert das alte
  Verzeichnis nicht, wird nur die Config geändert. Ohne Option bricht `config` ab.
* **Meldungen:** Leere Patch-Verzeichnisse werden als INFO statt ERROR gemeldet; einige
  Texte sind präziser bzw. korrigiert (z. B. „Could not find the base ref …“).
* **`create-patch`:** neue Option `-n`/`--name`; die wirkungslose Option `-U` entfällt.

Schon vorher:

* `clean` folgt beim rekursiven Löschen keinen Symlinks (Java/`File.isDirectory` tut das und
  kann so Dateien außerhalb von `_workdir` löschen).
* Nach einem fehlgeschlagenen `apply` bleibt kein `.git/index.lock` liegen (JGit-Bug, der
  danach `reset` scheitern lässt).
* Patches werden nach ihrer Nummer sortiert angewendet (Java: Dateisystem-Reihenfolge).
* Nicht unterstützt: Rename-Erkennung via `diff.renames`, `GIT binary patch`-Hunks,
  kombinierte/traditionelle Diffs beim Anwenden, `core.autocrlf`/`.gitattributes`-EOL und
  Clean/Smudge-Filter (z. B. LFS). Detailtexte von Git-Fehlern stammen von go-git.
* Randfälle mit Go-Standardmitteln: Trimmen mit `strings.TrimSpace`, ungültiges UTF-8 wird
  pro Byte ersetzt, ungültige Kalendertage im `Date:`-Header werden abgelehnt.
