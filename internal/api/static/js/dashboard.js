// dashboard.js — interactions locales de l'intra : copier l'URL d'un dépôt
// dans le presse-papier, et la notation par étoiles du feedback correcteur.
// Délégation d'événements pour survivre aux swaps htmx (tuiles ré-rendues).

(function () {
  "use strict";

  // --- Copier repo ---
  async function copyRepo(btn) {
    const url = btn.dataset.repo;
    if (!url) return;
    try {
      await navigator.clipboard.writeText(url);
    } catch (_) {
      // Repli si le presse-papier n'est pas dispo (http, permissions) :
      const tmp = document.createElement("textarea");
      tmp.value = url;
      tmp.style.position = "fixed";
      tmp.style.opacity = "0";
      document.body.appendChild(tmp);
      tmp.select();
      try { document.execCommand("copy"); } catch (_) {}
      document.body.removeChild(tmp);
    }
    const original = btn.textContent;
    btn.textContent = "Copié ✓";
    btn.classList.add("copied");
    setTimeout(() => {
      btn.textContent = original;
      btn.classList.remove("copied");
    }, 1400);
  }

  // --- Étoiles feedback ---
  function paintStars(group, value) {
    group.querySelectorAll(".star").forEach((s) => {
      s.classList.toggle("on", Number(s.dataset.val) <= value);
    });
  }

  function setRating(star) {
    const group = star.closest(".feedback-rating");
    if (!group) return;
    const value = Number(star.dataset.val);
    const hidden = group.querySelector('input[name="rating"]');
    if (hidden) hidden.value = String(value);
    paintStars(group, value);
  }

  // Peint l'état initial (5★ par défaut) des formulaires présents.
  function initStars(root) {
    (root || document).querySelectorAll(".feedback-rating").forEach((group) => {
      const hidden = group.querySelector('input[name="rating"]');
      paintStars(group, hidden ? Number(hidden.value) : 5);
    });
  }

  document.addEventListener("click", (e) => {
    const copyBtn = e.target.closest(".copy-repo");
    if (copyBtn) { copyRepo(copyBtn); return; }
    const star = e.target.closest(".feedback-rating .star");
    if (star) { setRating(star); }
  });

  document.addEventListener("DOMContentLoaded", () => initStars(document));
  // Après un swap htmx (nouvelle tuile / formulaire), re-peindre les étoiles.
  document.body.addEventListener("htmx:afterSwap", (e) => initStars(e.target));
})();
