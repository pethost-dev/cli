# pethost: put a folder online from your terminal

`pethost` is the command line of [Pethost](https://pethost.dev), a small cloud for personal
projects, easy to use for you and your agent. One flat price rents a machine of your own, and
it runs anything Docker runs. With `pethost` you put a folder online with one command, and see
and change what runs there: logs, requests, a shell, secrets, a domain, a rollback.

```sh
curl -fsSL https://pethost.dev/install.sh | sh                 # macOS, Linux
irm https://pethost.dev/install.ps1 | iex                      # Windows, in PowerShell
go install github.com/pethost-dev/cli/cmd/pethost@latest       # anywhere with Go
```

```
pethost 0.1.0 is in ~/.local/bin (checksum verified).
Next: cd into your app, and run   pethost deploy
```

The script downloads one program, checks it against the release's checksums and puts it in
`~/.local/bin`. It needs no `sudo` and changes nothing else. Later, `pethost update` replaces
the program with the newest release.

You need a Pethost account with a plan: [get a machine](https://console.pethost.dev/). If you
have none yet, `pethost deploy` shows you where to choose one, and waits.

Every screen below is the real output of the command above it. The project is a folder named
`recipes` that holds a small site, on the account whose domain is `sam.pethost.app`.

- [How do I put a folder online?](#how-do-i-put-a-folder-online)
- [What if my folder has no Dockerfile?](#what-if-my-folder-has-no-dockerfile)
- [What is running?](#what-is-running)
- [Why is my site broken?](#why-is-my-site-broken)
- [Where are the logs?](#where-are-the-logs)
- [Where do I put secrets?](#where-do-i-put-secrets)
- [Can I use a domain of my own?](#can-i-use-a-domain-of-my-own)
- [How do I reach the database from my laptop?](#how-do-i-reach-the-database-from-my-laptop)
- [How do I go back a version?](#how-do-i-go-back-a-version)
- [How do I deploy from GitHub?](#how-do-i-deploy-from-github)
- [How do I deploy from CI?](#how-do-i-deploy-from-ci)
- [How do I use it with an AI agent?](#how-do-i-use-it-with-an-ai-agent)
- [Every command](#every-command)

## How do I put a folder online?

Go to your app's folder and run `pethost deploy`.

The folder needs a `Dockerfile`: a short file that says how your app is built and started. A
folder with a `compose.yaml` works too. If it has neither, run the command anyway: for most
projects `pethost` [writes the Dockerfile itself](#what-if-my-folder-has-no-dockerfile).

```
$ cd recipes
$ pethost deploy
Name it [recipes]:                   it will live at https://recipes.sam.pethost.app
✓ Packed 3 files, 503 B
✓ Uploaded straight to your machine
✓ Built and started in 1 s
  · wrote compose.yaml for the Dockerfile: the service app, reading .env
✓ app listens on 3000 → https://recipes.sam.pethost.app
  It answers 200.

https://recipes.sam.pethost.app

Wrote .pethost (commit it). Next:   pethost logs -f   ·   pethost open
```

- **It asks one thing: the name.** Press Enter to take the folder's. The name becomes the
  address and cannot change later.
- **The first time, it signs you in.** Your browser opens the Pethost panel: press Connect
  there, and the deploy goes on.
- **A line that starts with `·`** is your machine saying what it did by itself. Here it found
  a `Dockerfile` and no `compose.yaml`, so it wrote one.
- **The address is given to you**: the project's name under your account's domain, with HTTPS.
- **`.pethost`** is a small file the command wrote in the folder. It says which project this
  folder is. Commit it with your code.

After a change, run the same command again. It is the project's new version:

```
$ pethost deploy
recipes (.pethost)
✓ Packed 5 files · uploaded · built and started in 2 s
  · kept the project's .env
  · kept the project's x-pethost
  It answers 200.
https://recipes.sam.pethost.app
```

The first line of every command says which project it acts on, and how it knows.

## What if my folder has no Dockerfile?

Run `pethost deploy` all the same. Your machine refuses the folder, and `pethost` then looks
at it. Where the folder is a project of a kind it knows, it writes the `Dockerfile`, says so
in a line, and deploys. Here the folder is what `npm create vite` makes:

```
$ pethost deploy
Name it [recipes]:                   it will live at https://recipes.sam.pethost.app
✓ Packed 19 files, 94 KB
✓ Uploaded straight to your machine
✗ /compose.yaml: the project directory has no compose.yaml at its root: add one
✓ Vite app detected, wrote Dockerfile.
✓ Packed 20 files, 95 KB
✓ Uploaded straight to your machine
✓ Built and started in 6 s
  · wrote compose.yaml for the Dockerfile: the service app, reading .env, port 80
✓ app listens on 80 → https://recipes.sam.pethost.app
  It answers 200.

https://recipes.sam.pethost.app

Wrote .pethost (commit it). Next:   pethost logs -f   ·   pethost open
```

- **It knows a short list of project kinds**, and writes each kind's file: sites that a
  build turns into files (VitePress, Vite, Astro, Docusaurus, Eleventy, Create React App, a
  folder of plain pages), Node programs (whatever `package.json` starts with its `start`
  script: Next.js, NestJS, Express; and Nuxt), Python (Django, FastAPI, Flask, Streamlit,
  Gradio, a program), PHP (Laravel and Symfony as their installers make them, plain PHP pages)
  and Go. It takes from the folder what such a project declares: the package manager from
  the lockfile, the version from `.nvmrc` or `.python-version`, the build and the start from
  `package.json` or a `Procfile`. It asks you nothing.
- **Where the folder is none of these, it writes nothing and says why**: a framework it does
  not know, an app kept in a file of another name, two projects in one folder, a project
  that keeps its own Dockerfile in a subfolder, a database that has to run beside the
  program (which takes a `compose.yaml`). You write the file, or the coding agent on your
  computer does: it is given the reason.
- **Where your project has to change first, it says what**: a site built for a path below
  its address, a Django project whose settings allow no host, a Laravel project without its
  key.
- **The file is proven by its first deploy.** If your machine cannot build it, or the
  program does not start, `pethost` takes its Dockerfile back out of the folder, shows what
  failed, and offers your coding agent the folder with that error. A wrong file is never left
  behind.
- **Then the file is yours.** It is an ordinary `Dockerfile`, with a comment where a step
  needs one. Read it, change it, commit it. `pethost` never writes it again.
- **A line that starts with `~`** is something no Dockerfile can give your project, which
  you may have to act on: a variable to set, data that a new version would lose.

For a folder it does not know, it offers a coding agent of yours, if this
computer has Claude Code, Codex or pi: you choose one, once, and the agent reads the folder
and writes the `Dockerfile`. The command it is run with is shown: Claude Code and pi get no shell, Codex
runs in its own sandbox, and none gets your Pethost token. What it wrote is deployed only
when you say yes. `PETHOST_AGENT=codex` names one for a run, and `PETHOST_AGENT=none` turns
that off. In a script no agent is offered: a project of a known kind still gets its file, and
anything else ends the command with the machine's refusal.

## What is running?

`pethost` alone, in a project's folder, shows that project. It only reads: it never changes
anything.

```
$ pethost
recipes (.pethost)
Starter · 2 CPU, 3 GB · sam.pethost.app · memory 1.4 MB of 3 GB · disk 0% · backups on
recipes · https://recipes.sam.pethost.app
app   running   since 20:30   listens on 3000   memory 424 KB
1 service runs · last deploy succeeded 8 s ago
```

`pethost ls` shows the machine and every project on it, from any folder:

```
$ pethost ls
Starter · 2 CPU, 3 GB · sam.pethost.app · memory 1.4 MB of 3 GB · disk 0% · backups on
hello     1 running   https://hello.sam.pethost.app     3 requests, last 24 h
recipes   1 running   https://recipes.sam.pethost.app
```

Outside a project's folder, name the project with `-p`: `pethost logs -p recipes`. Every
command takes it.

## Why is my site broken?

`pethost why` puts on one screen what your machine already knows: its problems (a service that
keeps restarting, a deploy that failed), the paths that fail, and what the service wrote at the
moment a request failed.

```
$ pethost why
recipes (.pethost)
✗ GET /cgi-bin/search failed 5 of 5 times in the last 24 hours, last at 20:31:26
    app | 20:31:26 [::ffff:10.192.2.3]:58648: url:/cgi-bin/search
    app |          grep: /data/recipes.txt: No such file or directory
~ Of the 3 busiest paths the slowest is GET /cgi-bin/search: 95% answer within 4 ms (5 requests)
1 service runs · last deploy succeeded 29 s ago
Next:   pethost requests --status 5xx
```

Here the search page fails every time, and the line under it is why: the app looks for a file
that is not there. `pethost why` says what it saw, never a guess at the cause.

## Where are the logs?

`pethost logs` prints what your app wrote, the newest lines last:

```
$ pethost logs -n 6
recipes (.pethost)
20:31:24 app  [::ffff:10.192.2.3]:58624: url:/
20:31:24 app  [::ffff:10.192.2.3]:58624: response:200
20:31:25 app  [::ffff:10.192.2.3]:58634: url:/
20:31:25 app  [::ffff:10.192.2.3]:58634: response:200
20:31:26 app  [::ffff:10.192.2.3]:58648: url:/cgi-bin/search
20:31:26 app  grep: /data/recipes.txt: No such file or directory
```

- `pethost logs -f` goes on printing new lines until you press Ctrl-C.
- `pethost logs --since 1h --grep recipes` searches the last hour:

  ```
  $ pethost logs --grep recipes --since 1h
  recipes (.pethost)
  20:31:08 app  grep: /data/recipes.txt: No such file or directory
  20:31:08 app  grep: /data/recipes.txt: No such file or directory
  20:31:08 app  grep: /data/recipes.txt: No such file or directory
  ```

- `pethost logs --deploy` is the log of the last deploy: the build, then the start.

Your machine also keeps every request to the site. `pethost requests` shows them:

```
$ pethost requests --status 5xx
recipes (.pethost)
5 requests that answered 5xx in the last 24 hours · half answer within 2 ms, 95% within 4 ms
Busiest paths
  GET /cgi-bin/search   5 requests   half within 2 ms   95% within 4 ms
Latest requests
20:31:08  500  GET /cgi-bin/search?q=soup      2 ms  app  203.0.113.7
20:31:08  500  GET /cgi-bin/search?q=pie       1 ms  app  203.0.113.7
20:31:08  500  GET /cgi-bin/search?q=soup      1 ms  app  203.0.113.7
20:31:23  500  GET /cgi-bin/search?q=soup      4 ms  app  203.0.113.7
20:31:26  500  GET /cgi-bin/search?q=soup      4 ms  app  203.0.113.7
```

## Where do I put secrets?

In the project's variables, with `pethost env set`. It asks for the value and shows none of it,
so the secret is in no command line and no shell history:

```
$ pethost env set STRIPE_KEY
recipes (.pethost)
STRIPE_KEY: ••••••••
✓ /.env has STRIPE_KEY now. Deployed in 2 s.
```

The app has the variable from that moment. `pethost env` lists the names, and which service
reads each:

```
$ pethost env
recipes (.pethost)
STRIPE_KEY   app
Their values:  pethost env --show
```

- A value that is no secret can go on the line: `pethost env set LOG_LEVEL=debug`.
- `pethost env rm STRIPE_KEY` removes one.
- The variables live on your machine, in the project's `/.env`. A deploy keeps them: the
  folder's own `.env` is sent with the first deploy only. `pethost env push` sends the folder's
  `.env` again, and asks first, naming what would change.

## Can I use a domain of my own?

Any name under your account's domain is an address at once, with HTTPS:

```
$ pethost domain add blog.sam.pethost.app
recipes (.pethost)
✓ blog.sam.pethost.app → app:3000
https://blog.sam.pethost.app
```

A domain you own takes one more step, which is yours: a DNS record where you manage the
domain.

```sh
pethost domain add recipes.example.com
```

The command prints the record to make: a CNAME from your name to your machine's name. The
certificate comes by itself once the name resolves. `pethost domain` lists the project's
addresses and their certificates:

```
$ pethost domain
recipes (.pethost)
https://recipes.sam.pethost.app   → app:3000   certificate until Nov 6 at 17:39
https://blog.sam.pethost.app      → app:3000   certificate until Nov 6 at 17:39
```

`pethost domain rm blog.sam.pethost.app` takes an address away. `pethost password` puts a
password page in front of the whole site, and `pethost password off` removes it.

## How do I reach the database from my laptop?

A database is one more service of the project. Write a `compose.yaml` in the folder that names
both, the app and the database, and deploy:

```yaml
services:
  app:
    build: .
    env_file: .env
  db:
    image: postgres:17
    environment:
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    volumes:
      - db:/var/lib/postgresql/data
volumes:
  db:
```

```
$ pethost env set POSTGRES_PASSWORD
recipes (.pethost)
POSTGRES_PASSWORD: ••••••••
✓ /.env has POSTGRES_PASSWORD now. Deployed in 2 s.

$ pethost deploy
recipes (.pethost)
✓ Packed 5 files · uploaded · built and started in 4 s
  · kept the project's .env
  · kept the project's x-pethost
  It answers 200.
https://recipes.sam.pethost.app
```

The database is not on the internet: only the app reaches it, at `db:5432`. `pethost tunnel`
brings its port to your laptop for as long as the command runs:

```
$ pethost tunnel db
recipes (.pethost)
✓ localhost:5432 → db:5432 on your machine. Ctrl-C closes it.
```

Now a program on your laptop, as `psql -h localhost -U postgres`, talks to the database on your
machine. For one command, no tunnel is needed:

```sh
pethost run db -- psql -U postgres -c 'select count(*) from recipes'
pethost shell db
```

`pethost run` runs one command in the container and exits as the command did. `pethost shell`
opens a shell there, in your terminal, with nothing to install.

What a database writes must outlive a deploy, so it goes into a volume, as `db:` above.
`pethost volume` lists the project's volumes, and `pethost backup` its nightly snapshots.

## How do I go back a version?

```
$ pethost rollback
recipes (.pethost)
✓ Back on the version deployed today at 20:32, in 1 s.
  /.env, the routes and the volumes' data stay as they are now.
  This folder is as it was: the next pethost deploy puts its code online again.
```

It asks nothing: a site that is down should be back fast, and the next `pethost deploy` undoes
it. Your machine keeps the three versions before the current one. `pethost rollback --list`
shows them, and `pethost rollback <version>` goes to one:

```
$ pethost rollback --list
recipes (.pethost)
ywqbxssunt   deployed today at 20:33   deployed the files of deploy pyy5qfxias again   runs now
s4tg2mg77x   deployed today at 20:33   recipes.tgz
cn5unw3v2r   deployed today at 20:32   changed /.env
Go back to one:  pethost rollback s4tg2mg77x
```

## How do I deploy from GitHub?

Deploy the folder once, then connect its repository:

```sh
pethost github                                   # the repositories Pethost can read
pethost github sam/recipes                       # from now on, every push deploys itself
pethost github sam/recipes --branch main --directory apps/web
pethost github off                               # stop: the files stay as they are
```

Pethost reads only the repositories you gave its GitHub App, in the panel. With a repository
connected, `pethost rollback` goes back to an earlier commit.

## How do I deploy from CI?

A program signs in with an API token. Make one in the [panel](https://console.pethost.dev),
in Settings under "API tokens", and give it to the job as `PETHOST_TOKEN`. On GitHub Actions:

```yaml
steps:
  - uses: actions/checkout@v7
  - run: curl -fsSL https://pethost.dev/install.sh | sh
  - run: ~/.local/bin/pethost deploy
    env:
      PETHOST_TOKEN: ${{ secrets.PETHOST_TOKEN }}
```

The `.pethost` you committed says which project the folder is. Where nobody is at a terminal,
`pethost` asks nothing, draws no spinner and prints no colour:

```
$ pethost deploy
recipes (.pethost)
Packed 5 files · uploaded · built and started in <1 s
  kept the project's .env
  kept the project's x-pethost
  It answers 200.
https://recipes.sam.pethost.app
```

Only the address is on stdout, so `url=$(pethost deploy)` holds it. Everything else is on
stderr. A deploy that failed exits 1 and prints why.

## How do I use it with an AI agent?

The easiest way is the plugin, which needs no terminal. Tell your agent:

> Install the Pethost plugin from https://github.com/pethost-dev/plugin for yourself.

An agent that works in a shell can also run `pethost` itself. For a program, every command is
the same:

- **`--json`** prints the API's own answer on stdout, and an error as one object on stderr:

  ```
  $ pethost status --json | jq -c '{project_id: .project.project_id, url: .project.url}'
  {"project_id":"recipes","url":"https://recipes.sam.pethost.app"}

  $ pethost logs -p nope --json
  {"code":"NOT_FOUND","message":"no project nope; the machine has: hello, recipes"}
  ```

- **Nothing is asked.** A command that needs a choice exits 2 and names the exact commands to
  run instead.
- **The exit code says what kind of end it was:**

  | Code | Meaning |
  |---|---|
  | 0 | Done. |
  | 1 | It failed, or the machine refused it. |
  | 2 | A choice is needed: which project, which service, a missing `--yes`. |
  | 3 | A person must act: sign in, or choose a plan. |
  | 4 | Try later: the project is busy, or the machine does not answer. |
  | 130 | Interrupted. |

  `pethost run` exits as the command in the container did, 124 when it timed out and 125 when
  `pethost` itself failed.

- **`pethost api <Method> '<json>'`** calls any method of the
  [API](https://pethost.dev/docs/api/), raw.

## Every command

```
$ pethost help
Pethost runs your projects on a machine of your own.

  Put a folder online         pethost deploy
  See what runs               pethost
  My site is broken           pethost why
  Read the logs               pethost logs
  Secrets                     pethost env set STRIPE_KEY
  A domain of my own          pethost domain add recipes.example.com
  The database on my laptop   pethost tunnel db
  Go back a version           pethost rollback

Deploy and look
  deploy        Put a folder online; again, a new version
  status        What runs: this folder's project, else the machine and every project
  ls            The machine and every project on it
  why           What is wrong or slow: problems, failing paths, the log lines near them
  logs          Container output: follow it, search it, or read the build log
  requests      HTTP traffic: totals, busiest paths, the latest requests
  open          Open the site in the browser

Reach into a container
  run           Run one command in a container: its output, its exit code
  shell         Open a shell in a container
  tunnel        Bring a service's port to localhost, as a database to your laptop
  files         List a directory or print a file: the project's, a container's, a volume's
  cp            Copy a file in or out of a container or a volume

Change a project
  env           The project's variables, names only
  domain        The project's addresses and their certificates
  password      Put a password page in front of the site, or take it away
  rename        Change the name people see
  rollback      Go back to the version before, or to one of --list
  restart       Restart the project's services, or some of them
  stop          Stop the project's services, or some of them
  start         Start the project's stopped services, or some of them
  backup        The project's snapshots
  volume        The project's volumes
  github        Deploy from a GitHub repository: list them, connect one, or stop
  delete        Delete the project

The machine, and this program
  machine       The machine: its resources, SSH keys and open sessions
  login         Sign in, in the browser
  logout        Forget this computer's sign-in
  whoami        Who is signed in, and where
  update        Replace this program with the newest release
  completion    Print a shell's completions: bash, zsh, fish or powershell
  api           Call any API method, raw
  help          What pethost does, or how one command is used

pethost help <command> shows how a command is used, with examples.
Every command takes   -p <project>   --json   -y, --yes   -q   --debug
```

- `pethost help <command>` shows how one command is used, and ends with examples.
- `pethost completion zsh` prints your shell's completions, which also complete the names of
  your projects and services.
- Once a day at most, in a terminal, `pethost` says when a newer release is out and offers to
  update. `PETHOST_NO_UPDATE_CHECK=1` turns that off.
- The source is this repository: Go, under the MIT licence.

## More

- [The panel](https://console.pethost.dev): the same account in the browser.
- [The plugin](https://github.com/pethost-dev/plugin): Pethost for your AI agent.
- [The SDKs](https://github.com/pethost-dev/sdk): the same API from TypeScript, Python, Go and
  Rust.
- [The API's reference](https://pethost.dev/docs/api/): every method, message and field.

## Docker Compose hosting at one flat price

Pethost is managed hosting for Docker Compose projects. A subscription rents one virtual machine
that only your account uses. Pethost's software on it deploys your projects, gives them HTTPS
addresses, keeps their request logs and backs them up every night. You run it through your
agent or from a web panel: both show the same projects, logs and requests.

- **A machine of your own:** nobody else's projects on it. No cold starts, nothing sleeps.
  Security updates install themselves.
- **HTTPS addresses:** anything.yourname.pethost.app at once, or your own domain with one DNS
  record. Certificates renew themselves. A password page in front of the whole site: one
  sentence to your agent.
- **Deploys from GitHub:** every push deploys itself, and an earlier commit is one step back.
- **Request logs:** every request with its status and time, the busiest paths and the error
  rate, kept for 30 days.
- **Nightly backups:** encrypted, stored off the machine, kept for 14 days.
- **SSH and SFTP** into any container, and a terminal in the panel.
- **Docker Compose, as written:** a project is a Compose file, and the same file runs anywhere
  else Docker does. A database is one click, Postgres or Valkey, with a page for connecting
  your apps, at no extra cost.
- **In the EU:** your data lives on your machine, and requests to your sites go straight to
  it, never through our panel.

**Pricing.** Starter is €9.99 a month for 2 vCPU, 3 GB of memory and 30 GB of SSD. Grow is
€17.99 for 4 vCPU, 6 GB and 60 GB. Scale is €59 for 8 vCPU, 30 GB and 200 GB. Prices are without
VAT. As many projects, containers and databases as fit: nothing is metered, and nothing is billed
on top. There is no free tier. [See the plans](https://pethost.dev/#pricing).

**Who it is for.** Solo developers and early startups who host several projects, bots or small
apps and want one fixed monthly bill, and people who build with an AI agent and want it to
deploy and run what it wrote. It is not for large teams whose applications need several regions
or autoscaling: the machine is the limit.

## How Pethost compares

| If you use | The difference |
|---|---|
| [Heroku](https://pethost.dev/about/#heroku), [Render](https://pethost.dev/about/#render), [DigitalOcean App Platform](https://pethost.dev/about/#digitalocean) | There each app, worker and database is one more line on the bill. On Pethost one flat price covers the whole machine: as many projects and databases as fit, and nothing sleeps. |
| [Railway, Fly.io](https://pethost.dev/about/#railway), [Koyeb](https://pethost.dev/about/#koyeb) | There CPU, memory and traffic are metered, so the bill moves with every busy month. On Pethost the machine is the limit: nothing is metered, nothing is billed on top. |
| [Vercel, Netlify](https://pethost.dev/about/#vercel) | Made for front ends and serverless functions: a bot, a long-running worker or a database is another service with its own price. Pethost runs the front end, the back end and the database on one machine. |
| [Coolify, Dokploy](https://pethost.dev/about/#coolify), [CapRover, Dokku, Easypanel](https://pethost.dev/about/#caprover) | Self-hosted: the server is yours to rent, update, secure and back up. Pethost is the machine and the panel together, looked after for you, and agents are first-class users, not an add-on. |
| [Sliplane](https://pethost.dev/about/#sliplane) | The closest: a flat price per server. There a Compose file is converted into its own services; Pethost runs a Compose project as written. |
| [Replit](https://pethost.dev/about/#replit) | You build and publish inside Replit, with its own agent. Pethost works with the agent you already use, and one flat price covers all your apps. |
| [PythonAnywhere](https://pethost.dev/about/#pythonanywhere) | Python only, no Docker. Pethost runs anything Docker runs, in any language. |
| [A VPS you set up yourself](https://pethost.dev/about/#vps) | The server is the cheap part. You still set up the proxy, DNS, certificates, backups and updates yourself, and fix them when they break. Pethost is the server with all of that done. |

[About Pethost](https://pethost.dev/about/) has each comparison in full, with prices.

## Questions

**Which AI agents does Pethost work with?**
Almost any agent that supports skills and MCP. The ones we tested ourselves: Claude Code, Codex,
Cursor, ChatGPT, Gemini CLI, OpenClaw, Hermes and Pi.

**What can I deploy?**
Anything that runs in Docker: web apps, APIs, bots, workers, databases. No Dockerfile? With the
plugin installed, your agent knows the details and writes it for you.

**Do I need an agent?**
No. The web panel does the same, and shows the same projects, logs and requests.

**Can I use my own domain?**
Yes. One DNS record, and the certificate comes by itself.

**Is there a free tier?**
No: €9.99 rents a real machine, so nothing sleeps. Cancel any time:
[refunds and cancellation](https://pethost.dev/refunds/) has the rules.

