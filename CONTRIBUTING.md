# Contributing

Thanks for your interest in fyne-pdf!

## Reporting issues

Use the issue templates. For rendering problems, the PDF file is usually the
single most useful thing — attach it, or a minimal file that shows the same
issue. Please make sure you are allowed to share it.

Problems inside the PDF renderer itself may belong to
[cera](https://github.com/timzifer/cera); if unsure, open the issue here.

## Development

```sh
go vet ./...
go test -race ./...
golangci-lint run
```

On Linux, Fyne needs the OpenGL/X11 development headers:

```sh
sudo apt-get install -y gcc libgl1-mesa-dev xorg-dev
```

## Pull requests

- Keep changes focused; one topic per PR.
- Add or update tests where it makes sense.
- Run `gofmt`, `go vet` and the tests before pushing — CI checks the same.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `chore:`, ...).

By contributing you agree that your contributions are licensed under the
[MIT License](LICENSE).
