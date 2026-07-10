(function () {
  // --- Thème clair / sombre ---
  var themeToggle = document.getElementById('theme-toggle');
  var themeIcon = document.getElementById('theme-icon');

  function renderThemeIcon(theme) {
    if (themeIcon) themeIcon.src = theme === 'dark' ? '/static/moon.svg' : '/static/sun.svg';
  }

  renderThemeIcon(document.documentElement.getAttribute('data-theme'));

  if (themeToggle) {
    themeToggle.addEventListener('click', function () {
      var root = document.documentElement;
      var next = root.getAttribute('data-theme') === 'light' ? 'dark' : 'light';
      root.setAttribute('data-theme', next);
      localStorage.setItem('ft-moulinette-theme', next);
      renderThemeIcon(next);
    });
  }

  // --- Grille d'exercices ---
  // De vrais boutons et un input caché piloté ici
  var exerciseButtons = document.querySelectorAll('.exercise-grid .exercise-btn');
  var exerciseValueInput = document.getElementById('exercise-value');

  exerciseButtons.forEach(function (btn) {
    btn.addEventListener('click', function () {
      var wasSelected = btn.classList.contains('selected');
      exerciseButtons.forEach(function (b) { b.classList.remove('selected'); });
      if (wasSelected) {
        exerciseValueInput.value = ''; // reclic sur le sélectionné = désélection
      } else {
        btn.classList.add('selected');
        exerciseValueInput.value = btn.dataset.value;
      }
    });
  });

  // --- Onglets de source (archive / lien git GitHub) ---
  var tabs = document.querySelectorAll('.source-tab');
  var panels = document.querySelectorAll('.source-panel');
  var archiveInput = document.getElementById('archive-input');
  var repoUrlInput = document.getElementById('repo-url-input');

  tabs.forEach(function (tab) {
    tab.addEventListener('click', function () {
      var target = tab.dataset.target;

      tabs.forEach(function (t) {
        t.classList.toggle('active', t === tab);
        t.setAttribute('aria-selected', t === tab);
      });
      panels.forEach(function (p) {
        p.hidden = p.dataset.panel !== target;
      });

      if (target === 'git') {
        if (archiveInput) archiveInput.required = false;
        if (repoUrlInput) repoUrlInput.required = true;
      } else {
        if (archiveInput) archiveInput.required = true;
        if (repoUrlInput) repoUrlInput.required = false;
      }
    });
  });

  // --- Visibilité du dépôt Git (public / privé) ---
  var visibilityPills = document.querySelectorAll('.repo-visibility-pill');
  var privateVisibilityPanel = document.querySelector('[data-visibility-panel="private"]');

  visibilityPills.forEach(function (pill) {
    pill.addEventListener('click', function () {
      var vis = pill.dataset.visibility;
      visibilityPills.forEach(function (p) {
        p.classList.toggle('active', p === pill);
        p.setAttribute('aria-selected', p === pill);
      });
      if (privateVisibilityPanel) privateVisibilityPanel.hidden = vis !== 'private';
    });
  });

  // --- Dropzone ---
  var dropzone = document.getElementById('dropzone');
  var label = document.getElementById('dropzone-label');
  var defaultLabel = label ? label.textContent : '';

  function updateLabel() {
    if (!archiveInput || !label) return;
    if (archiveInput.files.length) {
      label.textContent = archiveInput.files[0].name;
      dropzone.classList.add('has-file');
    } else {
      label.textContent = defaultLabel;
      dropzone.classList.remove('has-file');
    }
  }

  if (dropzone && archiveInput) {
    dropzone.addEventListener('click', function () { archiveInput.click(); });
    archiveInput.addEventListener('change', updateLabel);

    ['dragenter', 'dragover'].forEach(function (evt) {
      dropzone.addEventListener(evt, function (e) { e.preventDefault(); dropzone.classList.add('dragover'); });
    });
    ['dragleave', 'drop'].forEach(function (evt) {
      dropzone.addEventListener(evt, function (e) { e.preventDefault(); dropzone.classList.remove('dragover'); });
    });
    dropzone.addEventListener('drop', function (e) {
      if (e.dataTransfer.files.length) {
        archiveInput.files = e.dataTransfer.files;
        updateLabel();
      }
    });
  }

  // --- Réinitialisation du formulaire après une soumission réussie ---
  document.body.addEventListener('htmx:afterRequest', function (e) {
    if (e.detail.elt.tagName === 'FORM' && e.detail.successful) {
      if (archiveInput) archiveInput.value = '';
      if (repoUrlInput) repoUrlInput.value = '';
      var githubTokenInput = document.getElementById('github-token-input');
      if (githubTokenInput) githubTokenInput.value = '';
      if (privateVisibilityPanel) privateVisibilityPanel.hidden = true;
      visibilityPills.forEach(function (p) {
        var isPublic = p.dataset.visibility === 'public';
        p.classList.toggle('active', isPublic);
        p.setAttribute('aria-selected', isPublic);
      });
      updateLabel();
      exerciseButtons.forEach(function (b) { b.classList.remove('selected'); });
      if (exerciseValueInput) exerciseValueInput.value = '';
      errorBanner.style.display = 'none';
    }
  });

  // --- Affichage des erreurs serveur / réseau dans le bandeau ---
  var errorBanner = document.getElementById('error-banner');

  // Le champ exercice est un input caché :
  // on bloque l'envoi nous-mêmes si rien n'est sélectionné.
  document.body.addEventListener('htmx:beforeRequest', function (e) {
    if (e.target.tagName === 'FORM' && exerciseValueInput && !exerciseValueInput.value) {
      e.preventDefault();
      errorBanner.textContent = 'Choisis un exercice avant de lancer la correction.';
      errorBanner.style.display = 'block';
    }
  });

  document.body.addEventListener('htmx:responseError', function (e) {
    errorBanner.textContent = 'Erreur : ' + e.detail.xhr.responseText;
    errorBanner.style.display = 'block';
  });
  document.body.addEventListener('htmx:sendError', function () {
    errorBanner.textContent = 'Erreur réseau.';
    errorBanner.style.display = 'block';
  });
})();
