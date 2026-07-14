// Fond animé de la page de connexion : petits carrés (clin d'œil au style
// néo-brutaliste) reliés par des lignes, aux couleurs du thème courant.
// Volontairement minimal plutôt que particles.js : ~2 Ko, pause quand
// l'onglet est caché, densité adaptée à la taille d'écran, et désactivé
// si l'utilisateur préfère réduire les animations.
(function () {
  var canvas = document.getElementById('particles-canvas');
  if (!canvas) return;
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;

  var ctx = canvas.getContext('2d');
  var particles = [];
  var accent = '#a855f7';
  var running = true;
  var LINK_DIST = 140;
  var MOUSE_DIST = 180;
  // Position du curseur (null tant qu'il n'a pas bougé ou qu'il a quitté la
  // fenêtre) : le canvas est en pointer-events:none, on écoute donc window.
  var mouse = null;

  function readTheme() {
    var styles = getComputedStyle(document.documentElement);
    accent = styles.getPropertyValue('--accent').trim() || accent;
  }

  // Le toggle de thème change data-theme sur <html> : on relit les couleurs.
  new MutationObserver(readTheme).observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-theme'],
  });

  function resize() {
    canvas.width = window.innerWidth;
    canvas.height = window.innerHeight;
    // ~1 particule / 25 000 px², bornée : assez pour habiller l'écran,
    // jamais au point de coûter cher sur un grand moniteur.
    var count = Math.min(70, Math.max(20, Math.round(canvas.width * canvas.height / 25000)));
    while (particles.length < count) particles.push(spawn());
    particles.length = count;
  }

  function spawn() {
    return {
      x: Math.random() * canvas.width,
      y: Math.random() * canvas.height,
      vx: (Math.random() - 0.5) * 0.4,
      vy: (Math.random() - 0.5) * 0.4,
      size: 2 + Math.random() * 3,
    };
  }

  function step() {
    if (!running) return;
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    for (var i = 0; i < particles.length; i++) {
      var p = particles[i];
      // Légère attraction vers le curseur, plafonnée pour que les particules
      // gravitent autour de lui sans jamais s'y agglutiner.
      if (mouse) {
        var mdx = mouse.x - p.x;
        var mdy = mouse.y - p.y;
        var mdist = Math.sqrt(mdx * mdx + mdy * mdy);
        if (mdist < MOUSE_DIST && mdist > 30) {
          p.vx += (mdx / mdist) * 0.02;
          p.vy += (mdy / mdist) * 0.02;
        }
        // Plafond de vitesse : sans lui, l'attraction accélère indéfiniment.
        var speed = Math.sqrt(p.vx * p.vx + p.vy * p.vy);
        if (speed > 0.8) {
          p.vx = (p.vx / speed) * 0.8;
          p.vy = (p.vy / speed) * 0.8;
        }
      }
      p.x += p.vx;
      p.y += p.vy;
      if (p.x < 0 || p.x > canvas.width) p.vx = -p.vx;
      if (p.y < 0 || p.y > canvas.height) p.vy = -p.vy;
    }

    ctx.strokeStyle = accent;
    ctx.lineWidth = 1;
    for (i = 0; i < particles.length; i++) {
      for (var j = i + 1; j < particles.length; j++) {
        var dx = particles[i].x - particles[j].x;
        var dy = particles[i].y - particles[j].y;
        if (dx > LINK_DIST || dx < -LINK_DIST || dy > LINK_DIST || dy < -LINK_DIST) continue;
        var dist = Math.sqrt(dx * dx + dy * dy);
        if (dist > LINK_DIST) continue;
        ctx.globalAlpha = (1 - dist / LINK_DIST) * 0.25;
        ctx.beginPath();
        ctx.moveTo(particles[i].x, particles[i].y);
        ctx.lineTo(particles[j].x, particles[j].y);
        ctx.stroke();
      }
    }

    // Liaisons particule ↔ curseur, un peu plus marquées que les liaisons
    // entre particules pour que l'interaction se voie au premier survol.
    if (mouse) {
      for (i = 0; i < particles.length; i++) {
        var pdx = particles[i].x - mouse.x;
        var pdy = particles[i].y - mouse.y;
        if (pdx > MOUSE_DIST || pdx < -MOUSE_DIST || pdy > MOUSE_DIST || pdy < -MOUSE_DIST) continue;
        var pdist = Math.sqrt(pdx * pdx + pdy * pdy);
        if (pdist > MOUSE_DIST) continue;
        ctx.globalAlpha = (1 - pdist / MOUSE_DIST) * 0.45;
        ctx.beginPath();
        ctx.moveTo(particles[i].x, particles[i].y);
        ctx.lineTo(mouse.x, mouse.y);
        ctx.stroke();
      }
    }

    ctx.globalAlpha = 0.55;
    ctx.fillStyle = accent;
    for (i = 0; i < particles.length; i++) {
      var q = particles[i];
      ctx.fillRect(q.x - q.size / 2, q.y - q.size / 2, q.size, q.size);
    }
    ctx.globalAlpha = 1;

    requestAnimationFrame(step);
  }

  document.addEventListener('visibilitychange', function () {
    var wasRunning = running;
    running = !document.hidden;
    if (running && !wasRunning) requestAnimationFrame(step);
  });

  window.addEventListener('resize', resize);

  window.addEventListener('mousemove', function (e) {
    mouse = { x: e.clientX, y: e.clientY };
  });
  // Curseur sorti de la fenêtre ou onglet quitté : plus de point d'ancrage.
  document.addEventListener('mouseleave', function () { mouse = null; });
  window.addEventListener('blur', function () { mouse = null; });
  // Sur écran tactile, le doigt joue le rôle du curseur pendant le contact.
  window.addEventListener('touchmove', function (e) {
    mouse = { x: e.touches[0].clientX, y: e.touches[0].clientY };
  }, { passive: true });
  window.addEventListener('touchend', function () { mouse = null; });

  readTheme();
  resize();
  requestAnimationFrame(step);
})();
