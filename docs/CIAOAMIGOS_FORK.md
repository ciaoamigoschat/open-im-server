# Personalizzazioni del fork CiaoAmigos

Questo documento registra le modifiche mantenute nel fork
`ciaoamigoschat/open-im-server` rispetto a OpenIM upstream. Va aggiornato insieme
al codice quando una personalizzazione viene aggiunta, rimossa o modificata.

## Patch mantenute

Le patch applicate sopra `v3.8.3-patch.12`, nell'ordine, sono:

| Commit | Modifica | Motivazione |
| --- | --- | --- |
| `7c86710a1` | Push offline per Android in background. | Un client Android con WebSocket ancora connesso ma `isBackground=true` deve ricevere la push FCM. Il foreground e iOS restano invariati. |
| `337c06312` | Limite della sequenza di lettura al `maxSeq` della conversazione. | Impedisce di memorizzare un `hasReadSeq` impossibile, che causava incoerenze nella lettura della conversazione. |
| `77d23e88f` | Verifica amicizia con eccezione per i segnali di chiamata. | Blocca le chat private tra non amici senza interrompere le chiamate anonime. |
| `8feab23b1` | Moderazione realtime dei messaggi. | Applica rate limit, controlli anti-spam e provvedimenti temporanei prima dell'accodamento Kafka. |
| `8591ca514` | Inizializzazione del contesto delle API di moderazione. | Rende disponibili `operationID` e strumentazione Redis anche per le route amministrative `GET`, `PUT` e `DELETE`, evitando risposte `502` dal proxy NodeAuth. |
| `eb016569e` | Push offline per dispositivi mobili in background in presenza di altre sessioni. | Una sessione iOS o Android in primo piano dello stesso account non deve sopprimere la push destinata a un altro telefono in background. |

Questi commit devono essere mantenuti insieme durante un aggiornamento di
OpenIM. In particolare, non sostituire l'immagine del fork con l'immagine
ufficiale: si perderebbero la gestione Android e le altre correzioni locali.

## Moderazione realtime

La patch `8feab23b1` introduce il package isolato `internal/moderation` e lo
invoca da `internal/rpc/msg/send.go` dopo la validazione del messaggio e prima
di webhook, `MsgToMQ`, Kafka, persistenza e consegna. NodeAuth non partecipa al
percorso realtime: viene usato soltanto come proxy amministrativo autenticato.

La patch `8591ca514` inizializza `operationID` nel middleware amministrativo
della moderazione per tutti i metodi HTTP. L'ID ricevuto viene preservato; se
manca, OpenIM ne genera uno prima di accedere ai repository Redis.

La configurazione, le liste, i contatori, mute, ban temporanei, eventi e
statistiche sono conservati in Redis. Il servizio RPC mantiene configurazione,
parole e domini in RAM e riceve gli aggiornamenti tramite Redis Pub/Sub, con un
refresh periodico di sicurezza ogni 30 secondi. La policy Redis predefinita è
`FAIL_OPEN`.

La specifica operativa, inclusi endpoint, ruoli, configurazione iniziale,
metriche, rollout e rollback, è in [`MODERATION.md`](./MODERATION.md).

## Push Android in background

La modifica è in `internal/msggateway/hub_server.go`, con test in
`internal/msggateway/hub_server_test.go`. Rende eleggibile per la push un client
Android che ha dichiarato `isBackground=true` anche se il WebSocket non è ancora
stato chiuso.

Comportamento da preservare:

- Android foreground con WebSocket connesso: nessuna push FCM duplicata;
- Android background: push FCM consentita;
- Android offline: comportamento OpenIM originale;
- iOS background: push FCM consentita;
- più dispositivi dello stesso account: la presenza di una sessione mobile in
  background mantiene eleggibile l'utente per il push offline, anche quando
  un'altra sessione mobile ha ricevuto il messaggio via WebSocket.

## Sequenza di lettura della conversazione

`internal/rpc/msg/as_read.go` legge il `maxSeq` della conversazione e limita
`HasReadSeq` a tale valore prima di salvarlo. Se riceve un valore superiore,
scrive un warning diagnostico e usa `maxSeq`. Il test si trova in
`internal/rpc/msg/as_read_test.go`.

## Verifica amicizia e segnali di chiamata

Data di introduzione: 13 settembre 2026.

### Obiettivo

Impedire l'invio di normali messaggi privati tra utenti che non sono amici,
senza interrompere il flusso di chiamata anonima. Il mittente dei segnali resta
sempre l'utente OpenIM reale: non viene usato né distribuito un token
amministratore.

### Configurazione modificata

`friendVerify` è impostato a `true` in tutte le sorgenti di configurazione usate
dal progetto:

- `config/openim-rpc-msg.yml`;
- `deployments/deploy/openim-config.yml`;
- `deployments/templates/config.yaml`.

Con questa opzione OpenIM restituisce `ErrNotPeersFriend` per i messaggi privati
tra non amici. Restano invariati i bypass già previsti da upstream per l'utente
IM amministratore e per i content type di notifica.

### Eccezione CiaoAmigos

`internal/rpc/msg/verify.go` invoca `isAllowedNonFriendCallSignal` prima di
rifiutare un messaggio fra non amici. L'implementazione si trova in
`internal/rpc/msg/call_signal_verify.go`.

L'eccezione accetta esclusivamente messaggi OpenIM `Custom` con uno dei seguenti
protocolli:

| Description | Tipi ammessi | Scopo |
| --- | --- | --- |
| `ciaoamigos.anonymous-call-request.v1` | `request`, `accepted`, `passed`, `cancelled` | Ricerca e negoziazione della chiamata anonima. |
| `ciaoamigos.voice-call.v1` | `invite`, `answer`, `ice`, `reject`, `end`, `busy` | Segnalazione WebRTC della chiamata. |

La validazione è fail-closed:

- content type diverso da `Custom`, JSON malformato o campi sconosciuti vengono
  rifiutati;
- envelope e payload hanno un limite di 128 KiB;
- `callId`, `sentAt`, tipo e campi obbligatori devono essere validi;
- `invite` e `answer` richiedono una SDP non vuota;
- `ice` richiede un candidato valido;
- il payload anonimo accetta solo modalità `VOICE` e le durate supportate
  dall'app;
- gli altri protocolli custom e i normali messaggi chat non possono usare
  l'eccezione.

Il protocollo `ciaoamigos.voice-call.v1` è condiviso dalle chiamate anonime e
dalle chiamate avviate da una conversazione. Di conseguenza la segnalazione
vocale è consentita anche tra non amici, mentre testo, media e altri messaggi
privati restano bloccati.

### Perché non usare l'utente amministratore

Inviare i segnali come amministratore aggirerebbe `friendVerify`, ma cambierebbe
il `sendID`. Il client correla offer, answer e ICE con il mittente reale e con il
peer della chiamata; sostituirlo romperebbe questi controlli. Inoltre il token
amministratore deve restare esclusivamente sul backend e non deve mai essere
incluso nell'app.

### File e test

- `internal/rpc/msg/call_signal_verify.go`: parser e validazione dei protocolli
  ammessi;
- `internal/rpc/msg/call_signal_verify_test.go`: casi validi e tentativi di
  bypass;
- `internal/rpc/msg/verify.go`: integrazione nel controllo di amicizia.

Prima di pubblicare una nuova immagine del fork eseguire almeno:

```bash
go test ./internal/msggateway ./internal/push/... ./internal/rpc/msg
```

Verificare inoltre su due account non amici che:

1. un messaggio di testo venga rifiutato;
2. una richiesta anonima valida arrivi;
3. offer, answer, ICE e chiusura completino una chiamata;
4. un custom message con description sconosciuta venga rifiutato.

Una modifica ai payload nell'app richiede l'aggiornamento coordinato del
validator del fork e dei relativi test. Cambiare soltanto i file YAML non basta:
per rendere operativa l'eccezione bisogna compilare e distribuire l'immagine che
contiene il codice Go modificato, quindi riavviare il servizio RPC dei messaggi.
