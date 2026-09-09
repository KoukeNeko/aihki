<script lang="ts">
  const repo = 'https://github.com/KoukeNeko/taiga-cli'
  let audience = $state(0)
  let platform = $state(0)
  let copied = $state('')
  let menu = $state(false)
  const demos = [
    {
      name: 'Humans',
      label: 'Your everyday workflow, a few keystrokes away.',
      command: 'taiga issue list',
      output:
        'REF  SUBJECT                 STATUS  ASSIGNEE  VERSION\n#2   Add keyboard shortcuts  New               1\n#1   Fix token refresh       New               1',
      note: 'Captured from the installed v0.6.0 binary after creating two issues in a disposable local Taiga project.',
    },
    {
      name: 'Shell & CI',
      label: 'A contract your scripts can count on.',
      command: 'taiga issue view 1 --json --fields ref,subject,status,version',
      output:
        '{"data":{"ref":1,"status":"New","subject":"Fix token refresh","version":1},"meta":{"contract":1}}',
      note: 'Exact stdout from the recorded session: one JSON object, explicit fields, and contract version 1.',
    },
    {
      name: 'AI agents',
      label: 'Discover the command before taking action.',
      command: 'taiga schema issue view --json',
      output:
        '{"data":{"command":"issue view","description":"View an issue","safety":"read","idempotency":"idempotent","input_schema":{"properties":{"ref":{"type":"string"}},"required":["ref"],"type":"object"},"output_schema":{"…":"full schema in transcript"}},"meta":{"contract":1}}',
      note: 'Captured schema output, shortened only to fit the terminal. The downloadable transcript contains the complete response.',
    },
  ]
  const installs = [
    {
      name: 'macOS',
      manager: 'Homebrew',
      command: 'brew install koukeneko/tap/taiga',
      note: 'Also available on Linux with Homebrew.',
    },
    {
      name: 'Windows',
      manager: 'Scoop',
      command:
        'scoop bucket add koukeneko https://github.com/KoukeNeko/scoop-bucket\nscoop install taiga',
      note: 'Open a new terminal after installation to refresh your PATH.',
    },
    {
      name: 'Linux',
      manager: 'Verified installer',
      command:
        'curl -fsSL https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/install.sh | sh',
      note: 'Prefer your package manager? Signed APT and DNF repositories are available in the installation guide.',
    },
  ]
  const features = [
    {
      icon: '▤',
      title: 'Your whole workflow.',
      text: 'Projects, epics, stories, tasks, issues, sprints, and wiki pages. From the first idea to the last checkmark.',
      tags: ['Kanban & Scrum', 'Comments', 'Attachments'],
      color: 'mint',
    },
    {
      icon: '{ }',
      title: 'Made to be scripted.',
      text: 'Stable JSON contracts and fixed exit codes give your shell scripts and CI pipelines an interface they can rely on.',
      tags: ['JSON Schema', 'CI-ready', 'No daemon'],
      color: 'blue',
    },
    {
      icon: '⌁',
      title: 'At home on your server.',
      text: 'Connect to Taiga Cloud or your self-hosted instance. Keep different sites and projects in their own profiles.',
      tags: ['Self-hosted', 'Subpath support', 'Git-local defaults'],
      color: 'purple',
    },
  ]
  async function copy(text: string, id: string) {
    try {
      await navigator.clipboard.writeText(text)
      copied = id
    } catch {
      copied = 'failed'
    }
  }
</script>

<svelte:head
  ><title>Taiga CLI — Taiga, from your terminal.</title><meta
    name="description"
    content="An independent command-line client for Taiga. Manage your agile workflow and automate with stable JSON contracts, safe writes, and native binaries."
  /></svelte:head
>

<a class="skip" href="#main">Skip to content</a>
<header>
  <div class="nav wrap">
    <a class="brand" href="#main" aria-label="Taiga CLI home"
      ><span class="brand-mark">✳</span> Taiga <span>CLI</span></a
    >
    <button
      class="menu-button"
      aria-expanded={menu}
      aria-controls="navigation"
      onclick={() => (menu = !menu)}>{menu ? 'Close ✕' : 'Menu ☰'}</button
    >
    <nav id="navigation" class:open={menu} aria-label="Main navigation">
      <a href="#features" onclick={() => (menu = false)}>Features</a><a
        href="#examples"
        onclick={() => (menu = false)}>Examples</a
      ><a href={`${repo}/wiki`}>Docs ↗</a><a class="nav-github" href={repo}
        >GitHub ↗</a
      ><a class="button small" href="#install" onclick={() => (menu = false)}
        >Get started <span>→</span></a
      >
    </nav>
  </div>
</header>
<main id="main">
  <section class="hero grid-paper">
    <div class="wrap hero-layout">
      <div class="hero-copy">
        <a class="eyebrow pill" href={`${repo}/releases/latest`}
          ><span class="status-dot"></span> OPEN SOURCE. TERMINAL NATIVE.
          <span>↗</span></a
        >
        <h1>
          Taiga, from<br />your <span class="teal">terminal.</span><span
            class="cursor"
            aria-hidden="true">_</span
          >
        </h1>
        <p class="lead">Less clicking. More creating.</p>
        <p class="hero-description">
          Your projects, issues, and sprints, right where you work. One reliable
          CLI for humans, scripts, CI, and AI agents.
        </p>
        <div class="actions">
          <a class="button" href="#install"
            >Bring Taiga to your terminal <span>→</span></a
          ><a class="text-link" href={repo}>View on GitHub ↗</a>
        </div>
        <div class="quick-install">
          <span aria-hidden="true">$</span><code
            >brew install koukeneko/tap/taiga</code
          ><button
            aria-label="Copy Homebrew install command"
            onclick={() => copy(installs[0].command, 'hero')}
            >{copied === 'hero' ? '✓' : '⧉'}</button
          >
        </div>
        <div class="platform-note">
          macOS <span>·</span> Linux <span>·</span> Windows
          <span class="note-divider">|</span> amd64 & arm64
        </div>
      </div>
      <div class="hero-art">
        <div class="art-label">
          <span class="tiny-dot"></span> YOUR WORKFLOW, CONNECTED
        </div>
        <img
          src={`${import.meta.env.BASE_URL}hero.png`}
          alt="Taiga CLI connecting a Kanban board, API, wiki, security, and agile workflows on white graph paper"
          width="1672"
          height="941"
          fetchpriority="high"
        />
        <div class="floating-note">
          <span class="check-icon">✓</span>
          <div>
            <strong>Stay in your flow.</strong><small
              >Agile workflows, simplified.</small
            >
          </div>
          <span class="spark">✧</span>
        </div>
      </div>
    </div>
    <div class="wrap trust-strip">
      <span>ONE BINARY. ALL THE WAYS YOU WORK.</span>
      <div>
        <span>⌘ <b>Humans</b></span><span>›_ <b>Shell scripts</b></span><span
          >⑂ <b>CI pipelines</b></span
        ><span>✧ <b>AI agents</b></span>
      </div>
    </div>
  </section>

  <section class="wrap section" id="features">
    <div class="section-heading">
      <div>
        <p class="eyebrow">A LITTLE LESS FRICTION</p>
        <h2>Everything in its flow.</h2>
      </div>
      <p>Keep the tools you love.<br />Give your workflow a command line.</p>
    </div>
    <div class="feature-grid">
      {#each features as feature}<article class="feature-card {feature.color}">
          <div class="feature-icon" aria-hidden="true">{feature.icon}</div>
          <h3>{feature.title}</h3>
          <p>{feature.text}</p>
          <div class="tags">
            {#each feature.tags as tag}<span>{tag}</span>{/each}
          </div>
        </article>{/each}
    </div>
    <div class="workflow-line">
      <span>From backlog to done</span>
      <p>
        Projects <i>→</i> Stories <i>→</i> Tasks <i>→</i> Sprints <i>→</i>
        <strong>✓ Shipped</strong>
      </p>
      <a href={`${repo}/wiki`}>Explore the handbook ↗</a>
    </div>
  </section>

  <section class="demo-section" id="examples">
    <div class="wrap demo-layout">
      <div>
        <p class="eyebrow">ONE INTERFACE. YOUR WAY.</p>
        <h2>
          Human-readable.<br /><span class="teal">Machine-reliable.</span>
        </h2>
        <p class="section-description">
          A readable table for you. Structured JSON for your scripts.
          Discoverable schemas for your agents. The same CLI, wherever work
          happens.
        </p>
        <div class="audiences" aria-label="Example audience">
          {#each demos as demo, i}<button
              class:active={audience === i}
              aria-pressed={audience === i}
              onclick={() => {
                audience = i
                copied = ''
              }}><span>0{i + 1}</span>{demo.name}<span>↗</span></button
            >{/each}
        </div>
        <p class="demo-caption">{demos[audience].label}</p>
      </div>
      <div class="terminal">
        <div class="terminal-bar">
          <div class="window-dots"><i></i><i></i><i></i></div>
          <span>taiga — {demos[audience].name.toLowerCase()}</span><span>⌘</span
          >
        </div>
        <div class="terminal-body">
          <div class="terminal-comment"># {demos[audience].label}</div>
          <div class="terminal-command">
            <span>$</span><code>{demos[audience].command}</code><button
              aria-label="Copy example command"
              onclick={() => copy(demos[audience].command, 'demo')}
              >{copied === 'demo' ? '✓' : '⧉'}</button
            >
          </div>
          <pre>{demos[audience].output}</pre>
          <span class="terminal-prompt" aria-hidden="true">$ ▋</span>
        </div>
        <div class="terminal-foot">
          <span class="status-dot"></span> Recorded output · Taiga CLI v0.6.0
          <span>local Taiga 6.10.2</span>
        </div>
      </div>
    </div>
    <div class="wrap example-note">
      <p>{demos[audience].note}</p>
      <a href={`${import.meta.env.BASE_URL}cli-session.txt`} download
        >Download the redacted session transcript ↓</a
      >
    </div>
  </section>

  <section class="wrap section safety" id="safety">
    <div class="safety-art" aria-hidden="true">
      <div class="shield">✓</div>
      <div class="paper-stamp">⌁ &nbsp; inspect → verify → write</div>
      <span class="decoration">✧</span>
    </div>
    <div>
      <p class="eyebrow">CONFIDENCE, BUILT IN</p>
      <h2>Automation.<br />Without the surprises.</h2>
      <div class="safety-grid">
        <div>
          <h3><span>01</span> Preview your changes</h3>
          <p>
            Use <code>--dry-run</code> to inspect a mutation before any write request
            is sent.
          </p>
        </div>
        <div>
          <h3><span>02</span> Respect other edits</h3>
          <p>
            Conflicting changes to the same field are refused, so you can
            re-read and decide.
          </p>
        </div>
        <div>
          <h3><span>03</span> No blind write retries</h3>
          <p>
            An uncertain write reports <code>ambiguous_commit</code>. Verify the
            outcome before trying again.
          </p>
        </div>
        <div>
          <h3><span>04</span> Credentials stay private</h3>
          <p>
            Tokens go to your OS keyring. Sensitive secrets stay out of output,
            including dry runs.
          </p>
        </div>
      </div>
      <a class="text-link" href={`${repo}/wiki/Work-Items`}
        >Understand the safety model →</a
      >
    </div>
  </section>

  <section class="install-section grid-paper" id="install">
    <div class="wrap install-layout">
      <div>
        <p class="eyebrow">READY WHEN YOU ARE</p>
        <h2>
          Your next workflow<br />starts with
          <span class="teal">one command.</span>
        </h2>
        <p class="section-description">
          Native binaries for macOS, Linux, and Windows.<br />Install, connect
          your Taiga, and make yourself at home.
        </p>
        <a class="text-link" href={`${repo}/blob/main/INSTALL.md`}
          >All installation methods ↗</a
        >
      </div>
      <div class="install-card">
        <div class="install-tabs" aria-label="Installation platform">
          {#each installs as install, i}<button
              class:active={platform === i}
              aria-pressed={platform === i}
              onclick={() => {
                platform = i
                copied = ''
              }}>{install.name}</button
            >{/each}
        </div>
        <div class="install-content">
          <div class="install-label">
            <span>{installs[platform].manager}</span><button
              onclick={() => copy(installs[platform].command, 'install')}
              >{copied === 'install' ? 'Copied ✓' : 'Copy ⧉'}</button
            >
          </div>
          <pre>{installs[platform].command}</pre>
          <p>{installs[platform].note}</p>
          <div class="next-step">
            <span>02</span>
            <div>
              <strong>Connect to your Taiga</strong><code>taiga auth login</code
              >
            </div>
          </div>
          <div class="next-step">
            <span>03</span>
            <div>
              <strong>Choose your project, then explore</strong><code
                >taiga project use &lt;project-slug&gt;<br />taiga issue list</code
              >
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>

  <section class="wrap resources">
    <div>
      <span class="resource-icon">⌘</span>
      <h3>A little help along the way.</h3>
      <p>Guides for your first command and everything after.</p>
    </div>
    <a href={`${repo}/wiki`}>Read the handbook <span>↗</span></a><a
      href={`${repo}/blob/main/COMPATIBILITY.md`}
      >Check compatibility <span>↗</span></a
    ><a href={`${repo}/issues`}>Report an issue <span>↗</span></a>
  </section>
  <div class="wrap engineering">
    <span>Built with Go</span><span>MIT licensed</span><span
      >Taiga 6.10.2 verified</span
    ><a href={`${repo}/blob/main/README.md#diagnosable-when-something-breaks`}
      >Diagnostics with taiga doctor ↗</a
    >
  </div>
</main>
<footer class="wrap">
  <div>
    <a class="brand" href="#main"
      ><span class="brand-mark">✳</span> Taiga <span>CLI</span></a
    >
    <p>An independent client. Not affiliated with the Taiga project.</p>
  </div>
  <div>
    <a href={repo}>GitHub ↗</a><a href={`${repo}/blob/main/CHANGELOG.md`}
      >Changelog</a
    ><a href={`${repo}/blob/main/LICENSE`}>MIT License</a>
  </div>
</footer>
<div class="sr-only" role="status" aria-live="polite">
  {copied === 'failed'
    ? 'Copy unavailable. Please select and copy the command manually.'
    : copied
      ? 'Command copied to clipboard.'
      : ''}
</div>
