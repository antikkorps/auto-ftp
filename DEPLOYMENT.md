# Déploiement et utilisation sur site

Ce document s'adresse aux personnes qui **installent ou utilisent**
`auto-ftp.exe` sur un PC client. Pour les détails techniques (code,
build, tests), voir `README.md`.

## Installation

1. Télécharger la dernière version d'`auto-ftp.exe` depuis la page
   **Releases** du projet GitHub.
2. Créer un dossier dédié, par exemple `C:\EasyVIEW-FTP\`, et y déposer
   `auto-ftp.exe`. L'outil créera à côté :
   - un dossier `graphiques/` qui recevra les fichiers envoyés par
     EasyVIEW
   - un dossier `logs/` qui contient les journaux d'activité
3. Double-cliquer sur `auto-ftp.exe` pour le démarrer.
4. Au premier lancement, Windows peut afficher une alerte firewall :
   cliquer sur **Autoriser l'accès**, en cochant **Réseaux privés** et
   **Réseaux publics** pour ne rien bloquer.

Au prochain démarrage de Windows, l'outil se relance automatiquement
(via un raccourci déposé dans `%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\auto-ftp.vbs`).

## La fenêtre en un coup d'œil

- **Badge vert « SERVEUR EN LIGNE »** : le serveur FTP fonctionne.
- **Badge rouge** : quelque chose ne va pas. Un message précise
  pourquoi (`PORT 2121 DÉJÀ UTILISÉ`, `PERMISSION REFUSÉE`,
  `SERVEUR NE RÉPOND PLUS`, ou autre). Dans ce cas, suivre la section
  **Dépannage** plus bas.
- **Carte « À renseigner dans EasyVIEW »** : les 4 valeurs à saisir
  dans la VM (adresse IP, port, utilisateur, mot de passe). Le bouton
  icône à droite de chaque valeur la copie dans le presse-papiers.
- **Carte dossier de réception** : chemin du dossier où EasyVIEW
  déposera ses fichiers, ligne « Dernier fichier reçu » qui indique
  le dernier upload et depuis combien de temps, et trois boutons :
  - **Ouvrir le dossier** : ouvre `graphiques/` dans l'Explorateur.
  - **Voir les logs** : ouvre `logs\auto-ftp.log` dans le Bloc-notes.
  - **Arrêter le serveur** : coupe proprement le serveur et ferme
    l'application.
- **En bas à droite** : numéro de version (utile au support).

## Configurer EasyVIEW dans la VM

Dans EasyVIEW, configurer la sortie FTP avec les 4 valeurs affichées
par auto-ftp (les copier avec les boutons pour éviter les erreurs de
saisie). Les connexions entrantes apparaîtront dans les logs de
auto-ftp, et les fichiers transférés dans le dossier `graphiques/`.

## Vérifier que ça marche

1. La fenêtre affiche un badge **vert** et une IP locale.
2. Dans EasyVIEW, déclencher un envoi.
3. La ligne « Dernier fichier reçu » dans auto-ftp doit se mettre à
   jour avec le nom du fichier et « à l'instant ».
4. Ouvrir le dossier `graphiques/` : le fichier doit y être.

Si l'une de ces étapes échoue, passer au dépannage.

## Dépannage

### Le badge est rouge

Ouvrir **Voir les logs**, lire la ou les dernières lignes. Les erreurs
les plus fréquentes :

- `PORT 2121 DÉJÀ UTILISÉ` — une autre instance d'auto-ftp est déjà en
  cours (c'est le cas normal après un redémarrage grâce à l'autostart,
  le double-clic inutile sera intercepté), ou un autre logiciel utilise
  ce port. Vérifier la barre des tâches pour trouver la fenêtre
  existante.
- `PERMISSION REFUSÉE` — généralement un antivirus ou une politique
  d'entreprise bloque l'ouverture de port. Contacter le support.
- `SERVEUR NE RÉPOND PLUS` — le serveur a planté ou le firewall a
  coupé l'écoute. Fermer la fenêtre et relancer auto-ftp.

### Rien n'arrive dans `graphiques/`

- Vérifier dans la VM qu'EasyVIEW est bien configuré avec les valeurs
  affichées par auto-ftp.
- Ouvrir **Voir les logs** et chercher une ligne `auth failed` : si
  présente, c'est que l'identifiant ou le mot de passe saisi dans
  EasyVIEW ne correspond pas. Recopier les valeurs avec le bouton
  **Copier**.
- Si les logs ne contiennent aucun `client connected`, la VM n'atteint
  pas l'hôte : vérifier le réseau VirtualBox (NAT, bridged, host-only)
  et le firewall Windows de l'hôte.

### Je redémarre le PC, est-ce que ça reprend ?

Oui. L'application se relance automatiquement à l'ouverture de session
Windows grâce au raccourci déposé dans le dossier `Startup`.

### Je clique sur la croix, c'est grave ?

Non. Le serveur est arrêté proprement avant la fermeture (les
connexions en cours ne sont pas coupées brutalement). Au prochain
démarrage Windows, il repartira tout seul. Si vous voulez le relancer
tout de suite, double-cliquer sur `auto-ftp.exe`.

### Le double-clic n'ouvre rien

Si une instance tourne déjà (par exemple lancée par l'autostart au
boot), un second double-clic se contente de **ramener la fenêtre
existante au premier plan**. Si elle était minimisée ou cachée
derrière d'autres fenêtres, elle devrait apparaître.

## Désinstallation

1. Cliquer sur **Arrêter le serveur** dans la fenêtre.
2. Supprimer le dossier contenant `auto-ftp.exe`, `graphiques/` et
   `logs/`.
3. Supprimer le fichier
   `%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\auto-ftp.vbs`
   pour empêcher le redémarrage automatique (taper `shell:startup`
   dans la barre d'adresse de l'Explorateur pour y accéder).

## Contact support

En cas de problème non résolu par ce document :

1. Cliquer sur **Voir les logs**, sélectionner tout (`Ctrl+A`), copier.
2. Noter le numéro de **version** affiché en bas à droite de la
   fenêtre.
3. Transmettre les deux au support, avec une description de ce qui a
   été tenté.
