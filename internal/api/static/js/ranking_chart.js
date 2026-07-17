(function () {
  // Rend interactifs les graphes de progression : ligne-guide verticale au
  // survol, tooltip des valeurs par coalition à la date pointée, points mis en
  // avant, et légende cliquable pour masquer/afficher une courbe.

  var SVG_NS = 'http://www.w3.org/2000/svg';

  function svgPoint(svg, clientX, clientY) {
    var pt = svg.createSVGPoint();
    pt.x = clientX;
    pt.y = clientY;
    var ctm = svg.getScreenCTM();
    if (!ctm) return null;
    return pt.matrixTransform(ctm.inverse());
  }

  function nearestColumn(data, svgX) {
    var best = 0;
    var bestDist = Infinity;
    for (var i = 0; i < data.columns.length; i++) {
      var d = Math.abs(data.columns[i].x - svgX);
      if (d < bestDist) { bestDist = d; best = i; }
    }
    return best;
  }

  function initChart(card) {
    if (card.dataset.chartInit === '1') return;
    var raw = card.getAttribute('data-chart');
    if (!raw) return;
    var data;
    try { data = JSON.parse(raw); } catch (e) { return; }
    card.dataset.chartInit = '1';

    var svg = card.querySelector('svg.progress-chart');
    var cursor = svg.querySelector('[data-cursor]');
    var highlight = svg.querySelector('[data-highlight]');
    var tooltip = card.querySelector('[data-tooltip]');
    var hidden = {}; // index de série masquée -> true

    if (cursor) {
      cursor.setAttribute('y1', data.plotT);
      cursor.setAttribute('y2', data.plotB);
    }

    function render(col) {
      // Nettoie les points mis en avant du survol précédent.
      while (highlight.firstChild) highlight.removeChild(highlight.firstChild);

      var rows = [];
      var cursorX = null;
      for (var s = 0; s < data.series.length; s++) {
        if (hidden[s]) continue;
        var serie = data.series[s];
        for (var p = 0; p < serie.points.length; p++) {
          if (serie.points[p].col !== col) continue;
          var pt = serie.points[p];
          cursorX = pt.x;
          rows.push({ label: serie.label, color: serie.color, v: pt.v });

          var ring = document.createElementNS(SVG_NS, 'circle');
          ring.setAttribute('cx', pt.x);
          ring.setAttribute('cy', pt.y);
          ring.setAttribute('r', 6);
          ring.setAttribute('fill', serie.color);
          ring.setAttribute('stroke', 'var(--bg)');
          ring.setAttribute('stroke-width', '2');
          highlight.appendChild(ring);
        }
      }

      if (!rows.length || cursorX === null) { hideTooltip(); return; }

      if (cursor) {
        cursor.setAttribute('x1', cursorX);
        cursor.setAttribute('x2', cursorX);
        cursor.style.display = '';
      }

      var html = '<div class="chart-tooltip-date">' + data.columns[col].day + '</div>';
      for (var r = 0; r < rows.length; r++) {
        html += '<div class="chart-tooltip-row">' +
          '<span class="chart-tooltip-swatch" style="background:' + rows[r].color + '"></span>' +
          '<span class="chart-tooltip-label">' + rows[r].label + '</span>' +
          '<span class="chart-tooltip-val">' + rows[r].v + '</span></div>';
      }
      tooltip.innerHTML = html;
      tooltip.hidden = false;

      // Position du tooltip : ancrée sur la colonne, dans les bornes du plot.
      var rect = svg.getBoundingClientRect();
      var ratio = rect.width / 700;
      var left = cursorX * ratio + 12;
      if (left + tooltip.offsetWidth > rect.width) {
        left = cursorX * ratio - tooltip.offsetWidth - 12;
      }
      if (left < 0) left = 4;
      tooltip.style.left = left + 'px';
      tooltip.style.top = '8px';
    }

    function hideTooltip() {
      tooltip.hidden = true;
      if (cursor) cursor.style.display = 'none';
      while (highlight.firstChild) highlight.removeChild(highlight.firstChild);
    }

    function onMove(evt) {
      var touch = evt.touches && evt.touches[0];
      var cx = touch ? touch.clientX : evt.clientX;
      var cy = touch ? touch.clientY : evt.clientY;
      var loc = svgPoint(svg, cx, cy);
      if (!loc) return;
      render(nearestColumn(data, loc.x));
    }

    svg.addEventListener('mousemove', onMove);
    svg.addEventListener('mouseleave', hideTooltip);
    svg.addEventListener('touchstart', onMove, { passive: true });
    svg.addEventListener('touchmove', onMove, { passive: true });

    // Légende : clic pour masquer/afficher une série.
    var legendItems = card.querySelectorAll('.legend-item[data-series]');
    legendItems.forEach(function (item) {
      item.addEventListener('click', function () {
        var idx = item.getAttribute('data-series');
        var off = item.classList.toggle('off');
        hidden[idx] = off;
        svg.querySelectorAll('[data-series="' + idx + '"]').forEach(function (el) {
          el.style.display = off ? 'none' : '';
        });
        hideTooltip();
      });
    });
  }

  function initAll(root) {
    (root || document).querySelectorAll('.chart-card[data-chart]').forEach(initChart);
  }

  // Rendu initial (defer) + après chaque swap htmx du contenu du classement.
  if (document.readyState !== 'loading') initAll();
  else document.addEventListener('DOMContentLoaded', function () { initAll(); });
  document.body.addEventListener('htmx:afterSwap', function (e) { initAll(e.target); });
})();
