(function () {
  var cta = document.getElementById('login-cta');
  if (!cta) return;
  var label = document.getElementById('login-cta-label');
  var arrow = document.getElementById('login-cta-arrow');
  var spinner = document.getElementById('login-cta-spinner');
  var defaultLabel = label.textContent;

  cta.addEventListener('click', function () {
    cta.classList.add('loading');
    label.textContent = 'Connexion…';
    arrow.setAttribute('hidden', '');
    spinner.hidden = false;
  });

  window.addEventListener('pageshow', function () {
    cta.classList.remove('loading');
    label.textContent = defaultLabel;
    arrow.removeAttribute('hidden');
    spinner.hidden = true;
  });
})();
