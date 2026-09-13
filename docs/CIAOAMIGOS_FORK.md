# Personalizzazioni del fork CiaoAmigos

Questo documento registra le modifiche mantenute nel fork
`ciaoamigoschat/open-im-server` rispetto a OpenIM upstream. Va aggiornato insieme
al codice quando una personalizzazione viene aggiunta, rimossa o modificata.

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
go test ./internal/rpc/msg -run 'TestIsAllowedNonFriendCallSignal'
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
