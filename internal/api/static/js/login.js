(function () {
  var cta = document.getElementById('login-cta');
  if (!cta) return;
  var label = document.getElementById('login-cta-label');
  var arrow = document.getElementById('login-cta-arrow');
  var spinner = document.getElementById('login-cta-spinner');
  var defaultLabel = label.textContent;

  cta.addEventListener('click', function () {
    // Changement d'état visuel instantané au clic
    cta.classList.add('loading');
    label.textContent = 'Connexion…';
    arrow.setAttribute('hidden', '');
    spinner.hidden = false;
  });

  // Réinitialisation de l'état si l'utilisateur fait "Retour" dans son navigateur
  window.addEventListener('pageshow', function () {
    cta.classList.remove('loading');
    label.textContent = defaultLabel;
    arrow.removeAttribute('hidden');
    spinner.hidden = true;
  });
})();
