(function () {
  var cta = document.getElementById('login-cta');
  if (!cta) return;
  var label = document.getElementById('login-cta-label');
  var arrow = document.getElementById('login-cta-arrow');
  var spinner = document.getElementById('login-cta-spinner');
  var defaultLabel = label.textContent;

  var terminal = document.getElementById('login-terminal');
  var terminalOutput = document.getElementById('terminal-output');
  var streamInterval;

  function generateGarbage() {
    var chars = '0123456789ABCDEF';
    var str = '0x';
    for (var i = 0; i < 70; i++) {
      str += chars.charAt(Math.floor(Math.random() * chars.length));
      if (Math.random() > 0.85) str += ' ';
    }
    return str;
  }

  cta.addEventListener('click', function (e) {
    e.preventDefault();
    
    cta.classList.add('loading');
    label.textContent = 'Authentification…';
    arrow.setAttribute('hidden', '');
    spinner.hidden = false;

    terminal.classList.add('open');
    terminalOutput.innerHTML = '';

    streamInterval = setInterval(function() {
      var line = document.createElement('div');
      line.className = 'blur-line';
      line.textContent = generateGarbage();
      terminalOutput.appendChild(line);
      
      if (terminalOutput.childNodes.length > 8) {
        terminalOutput.removeChild(terminalOutput.firstChild);
      }
    }, 25);

    setTimeout(function() {
      clearInterval(streamInterval);
      window.location.href = cta.getAttribute('href');
    }, 1200);
  });

  window.addEventListener('pageshow', function () {
    cta.classList.remove('loading');
    label.textContent = defaultLabel;
    arrow.removeAttribute('hidden');
    spinner.hidden = true;
    terminal.classList.remove('open');
    terminalOutput.innerHTML = '';
    if (streamInterval) clearInterval(streamInterval);
  });
})();
