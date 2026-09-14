# Moderazione realtime CiaoAmigos

## Architettura

La moderazione viene eseguita nel processo `openim-rpc-msg`, direttamente prima
di `MsgToMQ`:

```text
Client -> OpenIM Gateway -> SendMsg -> ModerationService -> Redis
                                                   |
                                      ALLOW -------+--> Kafka/msgtransfer
                                      BLOCK/MUTE/BAN -> errore 1405
```

NodeAuth non viene chiamato durante l'invio. Le operazioni amministrative
seguono invece questo percorso:

```text
Admin panel -> NodeAuth -> controllo Moderator/Administrator
            -> token amministratore OpenIM server-side -> API /moderation
```

Il secret OpenIM non deve essere inserito nel browser, nel frontend o nel bundle
mobile.

## Controlli

L'ordine del percorso realtime è:

1. mute e ban già attivi;
2. rate limit atomico a 10 secondi, 60 secondi e 5 minuti;
3. normalizzazione Unicode, zero-width, accenti, separatori e leetspeak;
4. parole vietate exact/contains/compact;
5. hash SHA-256 e conteggio duplicati;
6. destinatari unici nella finestra configurata;
7. domini vietati e link sospetti;
8. telefono associato a richieste di contatto;
9. fuzzy matching solo sui termini abilitati;
10. score, decisione, provvedimento temporaneo e audit.

I messaggi di notifica OpenIM, i segnali custom e i messaggi inviati dagli
account amministrativi OpenIM non vengono trattati come testo utente. Un
messaggio bloccato restituisce `MessageModerated` (`1405`) e non viene accodato.

## Configurazione iniziale

La prima istanza crea automaticamente la configurazione in Redis. Un payload
completo compatibile con `PUT /moderation/config` è:

```json
{
  "enabled": true,
  "redisFailurePolicy": "FAIL_OPEN",
  "rateLimit": {
    "messages10Sec": 8,
    "messages60Sec": 25,
    "messages5Min": 80
  },
  "duplicate": {
    "windowSeconds": 300,
    "warningCount": 4,
    "blockCount": 6,
    "levels": [
      { "count": 4, "score": 15 },
      { "count": 6, "score": 40 },
      { "count": 10, "score": 80 }
    ]
  },
  "recipients": {
    "windowSeconds": 60,
    "warningCount": 10,
    "blockCount": 20,
    "levels": [
      { "count": 10, "score": 20 },
      { "count": 20, "score": 50 },
      { "count": 40, "score": 100 }
    ]
  },
  "fuzzyMatching": {
    "enabled": true,
    "minWordLength": 5,
    "maxDistanceMedium": 1,
    "maxDistanceLong": 2
  },
  "score": {
    "flood10Sec": 25,
    "flood60Sec": 40,
    "flood5Min": 60,
    "suspiciousLink": 20,
    "phoneSolicitation": 25,
    "duplicateRecipients": 25
  },
  "thresholds": {
    "log": 30,
    "block": 50,
    "mute5Minutes": 70,
    "mute1Hour": 90,
    "mute24Hours": 120,
    "review": 150,
    "tempBanSeconds": 604800
  }
}
```

`FAIL_OPEN` consente la chat se Redis non è disponibile e registra l'errore.
`FAIL_CLOSE` rifiuta invece il messaggio. Un ban automatico è sempre temporaneo;
un ban manuale con `durationSeconds: 0` non ha TTL e deve essere usato soltanto
da un amministratore.

## API OpenIM

Tutti gli endpoint richiedono un token appartenente a un ID configurato in
`IMAdminUserID`:

```text
GET    /moderation/config
PUT    /moderation/config
GET    /moderation/banned-words
POST   /moderation/banned-words
PUT    /moderation/banned-words/:id
DELETE /moderation/banned-words/:id
GET    /moderation/banned-domains
POST   /moderation/banned-domains
PUT    /moderation/banned-domains/:id
DELETE /moderation/banned-domains/:id
GET    /moderation/users/:userId/status
POST   /moderation/users/:userId/mute
POST   /moderation/users/:userId/unmute
POST   /moderation/users/:userId/ban
POST   /moderation/users/:userId/unban
GET    /moderation/events
GET    /moderation/stats
```

NodeAuth espone gli stessi percorsi sotto `/api/moderation`. I permessi
applicativi sono:

| Ruolo | Operazioni |
| --- | --- |
| `Moderator` | Stato utente, eventi, statistiche, mute e unmute. |
| `Administrator` | Tutte le operazioni, incluse configurazione, liste e ban. |

Payload provvedimenti:

```json
{ "durationSeconds": 300, "reason": "spam ripetuto" }
```

Filtri eventi supportati: `page`, `pageSize`, `userID`, `action` e `reason`.
L'audit non conserva il testo originale del messaggio.

## Metriche

Il processo RPC dei messaggi pubblica:

```text
moderation_check_duration_seconds
moderation_redis_duration_seconds
moderation_normalization_duration_seconds
moderation_rules_duration_seconds
```

## Build e rollout

Per questa patch l'immagine è costruita per `linux/amd64`:

```bash
docker buildx build --platform linux/amd64 --load \
  -t ciaoamigos/openim-server:v3.8.3-patch.12-moderation-8feab23b1 .
```

Artifact verificato il 14 settembre 2026:

```text
Immagine: ciaoamigos/openim-server:v3.8.3-patch.12-moderation-8feab23b1
Architettura: linux/amd64
Image ID: sha256:9bdc118ee0fbf6ee5be2f39695638b6bb539ae63b0cb3d41cee00cc5af95fbf3
Archivio: openim-server-v3.8.3-patch.12-moderation-8feab23b1-linux-amd64.tar.gz
SHA-256: ccb846dd2583eb79bb8f8a2a239f5232231ec520650efe203fe0cd75e09f06c7
```

### Stato produzione

Il rollout su produzione è stato completato il 14 settembre 2026. Il container
usa l'immagine e l'Image ID riportati sopra. Il backup del file di ambiente è:

```text
/opt/openim-docker/.env.before-moderation-20260914-211619
```

Le verifiche successive al deploy hanno confermato container `healthy`, zero
riavvii, tutti i servizi attivi secondo `mage check`, health check pubblico
`200 OK` e rifiuto senza token di `/moderation/config`. Quest'ultimo controllo
conferma che la route è presente e protetta dall'autenticazione amministrativa.

Prima del rollout creare un backup del file `.env` del deployment. Modificare
soltanto `OPENIM_SERVER_IMAGE`, poi ricreare esclusivamente OpenIM Server:

```bash
cd /opt/openim-docker
cp .env ".env.before-moderation-$(date +%Y%m%d-%H%M%S)"
docker compose config --quiet
docker compose up -d --no-deps --force-recreate openim-server
docker compose ps openim-server
docker exec openim-server mage check
docker logs --since 5m openim-server
curl -fsS https://im.ciaoamigos.it/health
```

Verificare infine un messaggio consentito, uno bloccato, un mute con TTL e il
refresh di una parola vietata senza riavvio.

## Rollback

Ripristinare nel file `.env` l'immagine precedente e ricreare il solo container:

```bash
cd /opt/openim-docker
docker compose up -d --no-deps --force-recreate openim-server
docker compose ps openim-server
docker exec openim-server mage check
```

Le chiavi Redis `moderation:*` possono restare presenti durante il rollback:
le versioni precedenti del fork non le leggono.
