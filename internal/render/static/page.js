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
      say(res.ok ? "Thanks, we'll take a look." : "Couldn't send the report. Try again later.");
    } catch {
      say("Couldn't send the report. Try again later.");
    }
  });
})();
