(function () {
  var controls = document.getElementById('ranking-controls');
  if (!controls) return;

  var tabs = document.querySelectorAll('.rank-tab');
  var examPicker = document.getElementById('exam-picker');
  var examPills = document.querySelectorAll('.exam-pill');

  // Le classement est toujours celui de la piscine en cours : la session est
  // déduite côté serveur, seul l'onglet (et l'exam) est choisi ici.
  var state = { tab: 'score', exam: '00' };

  function load() {
    var url = '/ui/classement?tab=' + encodeURIComponent(state.tab);
    if (state.tab === 'exam') {
      url += '&exam=' + encodeURIComponent(state.exam);
    }
    htmx.ajax('GET', url, { target: '#ranking-content', swap: 'innerHTML' });
  }

  tabs.forEach(function (tab) {
    tab.addEventListener('click', function () {
      state.tab = tab.dataset.tab;
      tabs.forEach(function (t) {
        t.classList.toggle('active', t === tab);
        t.setAttribute('aria-selected', t === tab);
      });
      examPicker.hidden = state.tab !== 'exam';
      load();
    });
  });

  examPills.forEach(function (pill) {
    pill.addEventListener('click', function () {
      state.exam = pill.dataset.exam;
      examPills.forEach(function (p) { p.classList.toggle('active', p === pill); });
      load();
    });
  });

  // htmx est chargé en defer avant ce script : dispo au moment où on s'exécute.
  load();
})();
