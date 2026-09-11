const platform = document.querySelector("#platform");
const command = document.querySelector("#install-command");
const note = document.querySelector("#platform-note");
const copy = document.querySelector("#copy-install");
const status = document.querySelector("#copy-status");
const releases =
  "https://github.com/lroolle/ihme-cli/releases/latest/download/";

platform.addEventListener("change", () => {
  const target = platform.value;
  if (target === "go") {
    command.textContent =
      "go install github.com/lroolle/ihme-cli/cmd/ihme@latest\nihme version";
    note.textContent =
      "Requires Go. Add GOBIN (or the bin directory under GOPATH) to your PATH.";
  } else if (target.startsWith("windows_")) {
    command.textContent = `Invoke-WebRequest ${releases}ihme_${target}.zip -OutFile ihme.zip\nExpand-Archive ihme.zip -DestinationPath ihme\n.\\ihme\\ihme.exe version`;
    note.textContent =
      "Run in PowerShell. Move ihme.exe into a directory on your PATH before the sign-in commands below.";
  } else {
    command.textContent = `curl -fL ${releases}ihme_${target}.tar.gz -o ihme.tar.gz\ntar -xzf ihme.tar.gz ihme\nsudo install -m 755 ihme /usr/local/bin/ihme\nihme version`;
    note.textContent =
      "Run in your shell. This installs ihme in /usr/local/bin.";
  }
  status.textContent = "";
});

copy.hidden = false;
copy.addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(command.textContent);
    status.textContent = "Install commands copied.";
  } catch {
    status.textContent =
      "Could not access the clipboard. Select and copy the commands above.";
  }
});
