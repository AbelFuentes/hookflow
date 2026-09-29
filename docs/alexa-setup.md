# Alexa setup

## 1. Create the skill

1. Open the [Alexa Developer Console](https://developer.amazon.com/alexa/console/ask) and choose **Create skill**.
2. Experience type: **Other**. Model: **Custom**. Hosting: **Provision your own**. Template: **Start from scratch**.
   (Choosing "Smart home" creates a different skill type without an interaction model editor.)
3. Locale: Spanish (MX).

## 2. Interaction model

Build → Interaction Model → **JSON Editor** → paste `skill/interaction-model.es-MX.json` → **Save Model** → **Build skill**.

The invocation name is `mi automatizador`. The skill name shown in the console is only a label.

## 3. Configure hookflow

Copy the Skill ID from the skills list and put it in `.env` (loaded by the Makefile):

```
HOOKFLOW_ALEXA_SKILL_ID=amzn1.ask.skill.xxxxxxxx
```

```bash
make run
# expected log: "alexa endpoint enabled"
```

## 4. Expose the local server

Create a free static domain in the ngrok dashboard (Domains), then:

```bash
ngrok config add-authtoken YOUR_TOKEN
ngrok http --url=YOUR-DOMAIN.ngrok-free.dev 8080
```

## 5. Endpoint

Build → **Endpoint**:

- Type: **HTTPS**
- Default Region: `https://YOUR-DOMAIN.ngrok-free.dev/alexa`
- Certificate: **"My development endpoint is a sub-domain of a domain that has a wildcard certificate from a certificate authority"**

Click **Save Endpoints**, reload the page and check that the URL and the option persisted.

## 6. Test

Test tab → **Development** → say:

```
dile a mi automatizador buenas noches
```

Expected hookflow logs:

```
"msg":"rule matched","rule":"good-night"
"msg":"buenas noches"
"msg":"http action ok"
```

The `http` action in `rules/rules.yaml` posts to `http://localhost:9999`; run any local receiver there, or remove the action.

## Troubleshooting

| Symptom | Cause |
|---|---|
| "No puedo conectarme con la Skill", nothing in the logs, empty JSON Output | Wrong certificate option in Endpoint, or Endpoint not saved |
| Endpoint URL stopped working | Free tunnels without a static domain change URL on every restart |
| `404` on `/alexa` | `HOOKFLOW_ALEXA_SKILL_ID` is not set, so the route is not registered |
| `400 invalid signature` | The request did not come from Alexa (for example a manual `curl`) |
| `alexa request for a different skill` | `HOOKFLOW_ALEXA_SKILL_ID` does not match the console |
| Alexa says "Listo." but nothing runs | Check for `no rule matched`; the rule `on.name` must equal the intent name (`GoodNightIntent`) |
| Alexa says "Algo falló" | An action failed; see `alexa event failed` in the logs |
