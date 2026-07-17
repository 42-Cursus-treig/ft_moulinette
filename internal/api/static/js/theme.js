(function () {
  var root = document.documentElement;

  var stored = localStorage.getItem('ft-moulinette-theme');
  var theme = stored || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
  root.setAttribute('data-theme', theme);

  function wire() {
    var toggle = document.getElementById('theme-toggle');
    var icon = document.getElementById('theme-icon');

    function renderIcon(t) {
      if (icon) icon.src = t === 'dark' ? '/static/img/moon.svg' : '/static/img/sun.svg';
    }
    renderIcon(root.getAttribute('data-theme'));

    if (toggle) {
      toggle.addEventListener('click', function () {
        var next = root.getAttribute('data-theme') === 'light' ? 'dark' : 'light';
        root.setAttribute('data-theme', next);
        localStorage.setItem('ft-moulinette-theme', next);
        renderIcon(next);
      });
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', wire);
  } else {
    wire();
  }
})();
