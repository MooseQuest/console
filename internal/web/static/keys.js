// Copy button for the one-time API key display. Optional: the key field is
// readonly and select-on-click, so the page works without this script. The
// value is only handed to the clipboard; it is never stored or sent anywhere.
(function () {
  var btn = document.getElementById("copy-secret");
  var field = document.getElementById("secret");
  var status = document.getElementById("copy-status");
  if (!btn || !field || !status || !navigator.clipboard) {
    return;
  }
  btn.hidden = false;
  btn.addEventListener("click", function () {
    navigator.clipboard.writeText(field.value).then(
      function () { status.textContent = "Copied"; },
      function () { status.textContent = "Copy failed. Select the key and copy it manually."; }
    );
  });
})();
