// Public page behavior: copy buttons, click counts, share and report.
(() => {
  const toast = document.getElementById("toast");
  let toastTimer;
  const say = (msg) => {
    toast.textContent = msg;
    toast.classList.add("visible");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => toast.classList.remove("visible"), 2200);
  };

  const preview = location.pathname.startsWith("/preview/");

  // Schedule badges in the owner preview: show times in the viewer's zone.
  for (const el of document.querySelectorAll("time[data-local-time]")) {
    const at = new Date(el.dateTime);
    if (!Number.isNaN(at.getTime())) {
      el.textContent = at.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
    }
  }

  document.addEventListener("click", async (event) => {
    const tracked = event.target.closest("[data-track]");
    if (tracked && !preview && navigator.sendBeacon) {
      navigator.sendBeacon("/t/" + tracked.dataset.track);
    }

    const copy = event.target.closest("[data-copy]");
    if (copy) {
      try {
        await navigator.clipboard.writeText(copy.dataset.copy);
        say("Copied " + copy.dataset.copy);
      } catch {
        say("Copy failed. Select the text instead.");
      }
      return;
    }

    const share = event.target.closest("[data-share]");
    if (share) {
      const url = share.dataset.share;
      if (navigator.share) {
        navigator.share({ title: share.dataset.title, url }).catch(() => {});
      } else {
        await navigator.clipboard.writeText(url).catch(() => {});
        say("Link copied");
      }
      return;
    }

    if (event.target.closest("[data-report]")) {
      document.getElementById("report-dialog").showModal();
      return;
    }
    if (event.target.closest("[data-close]")) {
      event.target.closest("dialog").close();
    }
  });

  // Email sign-up forms: double opt-in, so the server only sends a link.
  for (const signup of document.querySelectorAll("form[data-signup]")) {
    const status = signup.querySelector(".signup-status");
    const show = (msg, error) => {
      status.textContent = msg;
      status.classList.toggle("error", !!error);
    };
    signup.addEventListener("submit", async (event) => {
      event.preventDefault();
      const input = signup.elements.email;
      if (!input.value.trim() || !input.checkValidity()) {
        show("Enter a valid email address.", true);
        input.focus();
        return;
      }
      if (preview) {
        show("Sign-ups are disabled in preview.", true);
        return;
      }
      const button = signup.querySelector("button[type=submit]");
      button.disabled = true;
      try {
        const res = await fetch("/api/subscribe/" + signup.dataset.signup, {
          method: "POST",
          headers: { "Content-Type": "application/json", "X-Requested-With": "zelda" },
          body: JSON.stringify({ email: input.value.trim(), website: signup.elements.website.value }),
        });
        const body = await res.json().catch(() => ({}));
        if (res.ok) {
          show(body.message || "Check your inbox to confirm.");
          signup.reset();
        } else {
          show(body.error?.message ? body.error.message.charAt(0).toUpperCase() + body.error.message.slice(1) + "." : "Couldn't sign you up. Try again later.", true);
        }
      } catch {
        show("Couldn't sign you up. Try again later.", true);
      } finally {
        button.disabled = false;
      }
    });
  }

  const form = document.getElementById("report-form");
  form?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    if (preview) {
      form.closest("dialog").close();
      say("Reports are disabled in preview");
      return;
    }
    try {
      const res = await fetch("/api/report", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Requested-With": "zelda" },
        body: JSON.stringify({ slug: form.dataset.slug, reason: data.get("reason"), details: data.get("details") }),
      });
      form.closest("dialog").close();
      say(res.ok ? "Report sent." : "Couldn't send the report. Try again later.");
    } catch {
      say("Couldn't send the report. Try again later.");
    }
  });
})();
