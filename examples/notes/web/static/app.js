// A form button stays off while its form posts, so a double click sends
// one request.
document.addEventListener("submit", (event) => {
  for (const button of event.target.querySelectorAll("button")) {
    button.disabled = true
  }
})
