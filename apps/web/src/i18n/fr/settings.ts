import type en from "../en/settings";
import type { Translation } from "../types";

export default {
  "settings.language": "Langue",
  "settings.languageAuto": "Comme le navigateur",

  "settings.groupUser": "Paramètres utilisateur",
  "settings.groupApp": "Paramètres de l'appli",
  "settings.sectionProfile": "Profil",
  "settings.sectionAccount": "Compte",
  "settings.sectionVoice": "Voix et vidéo",
  "settings.sectionNotifications": "Notifications",
  "settings.sectionAppearance": "Apparence",
  "settings.categoriesLabel": "Catégories de {title}",
  "settings.categoryHeading": "{title} : {category}",
  "settings.editProfile": "Modifier le profil ›",
  "settings.logOut": "Se déconnecter",
  "settings.footer": "Kallisti : un serveur et un client Mumble. {link}",

  "settings.aboutMe": "À propos de moi",
  "settings.bioHint": "Tout le monde sur le serveur le voit, aussi dans les applis Mumble.",
  "settings.myEmoji": "Mes émojis",
  "settings.myEmojiHint":
    "Écris :nom: pour en utiliser un. Tout le monde les voit dans tes messages et tes réactions ; les applis Mumble affichent :nom:. Pour en ajouter un depuis un message, clique dessus.",
  "settings.profilePicture": "Photo de profil",
  "settings.changePicture": "Changer la photo",
  "settings.choosePicture": "Choisir une photo",
  "settings.pictureError": "Impossible d'utiliser cette image. Essaie un fichier PNG, JPEG, GIF ou WebP.",
  "settings.pictureRegisterHint": "Crée un compte pour définir une photo de profil.",

  "settings.keepAccount": "Garder ce compte",
  "settings.guestAccountHint":
    "C'est un compte invité : il est supprimé un jour après ta dernière visite. Avec un mot de passe, il reste, et tu peux te connecter sur d'autres appareils.",
  "settings.setPasswordTitle": "Définir un mot de passe",
  "settings.fediOnlyHint":
    "Tu te connectes avec ton compte du fédivers. Un mot de passe marche aussi dans les applis Mumble, avec ton nom. D'abord, tu confirmes avec ton serveur du fédivers.",
  "settings.changePassword": "Changer le mot de passe",
  "settings.setPassword": "Définir le mot de passe",
  "settings.currentPassword": "Mot de passe actuel",
  "settings.newPassword": "Nouveau mot de passe (8 caractères ou plus)",
  "settings.passwordSetKept": "Mot de passe défini : ton compte reste.",
  "settings.passwordSetFirst":
    "Mot de passe défini : tu peux aussi te connecter avec ton nom. Les autres appareils sont déconnectés.",
  "settings.passwordChanged": "Mot de passe changé. Les autres appareils sont déconnectés.",
  "settings.confirmWith": "Confirmer avec {host}",
  "settings.otherDevice": "Se connecter sur un autre appareil",
  "settings.otherDeviceHint": "Affiche un code ici et scanne-le avec ton téléphone : aucun mot de passe à taper là-bas.",
  "settings.otherDeviceScanHint": "Ou scanne le code qu'un autre appareil affiche sur son écran de connexion.",
  "settings.showLoginCode": "Afficher un code de connexion",
  "settings.scanCode": "Scanner un code",
  "settings.logOutGuestHint":
    "Te déconnecter met fin à ce compte invité (définis un mot de passe ci-dessus pour le garder).",
  "settings.logOutEverywhereHint":
    "« Partout » déconnecte aussi tes autres navigateurs et appareils, par exemple après avoir utilisé un ordinateur partagé.",
  "settings.logOutEverywhere": "Se déconnecter partout",

  "settings.fediAccount": "Compte du fédivers",
  "settings.fediPublic": "L'afficher sur mon profil, comme vérifié",
  "settings.fediUnlink": "Dissocier",
  "settings.fediUnlinking": "Dissociation…",
  "settings.fediOnlyLogin":
    "C'est ton seul moyen de connexion : définis un mot de passe ci-dessus pour le dissocier.",
  "settings.fediLinkHint": "Connecte-toi aussi avec ton compte Mastodon, Pleroma, GoToSocial ou un autre compte du fédivers.",
  "settings.fediPlaceholder": "mastodon.social ou @toi@mastodon.social",
  "settings.fediAddressLabel": "Ton serveur ou ton adresse du fédivers",
  "settings.fediLink": "Associer le compte",
  "settings.fediOpening": "Ouverture…",

  "settings.allowedSites": "Serveurs où tu te connectes avec ce compte",
  "settings.allowedSitesNotLoaded": "La liste n'a pas pu être chargée.",
  "settings.removing": "Retrait…",
  "settings.allowedSitesHint":
    "Un serveur retiré te redemande ton accord à ta prochaine connexion. Ton compte là-bas reste.",

  "settings.qrLoginTitle": "Se connecter avec un QR code",
  "settings.qrMaking": "Création du code…",
  "settings.qrCodeLabel": "Code de connexion",
  "settings.qrOfferHint":
    "Scanne ce code avec l'appli Kallisti (ou l'appareil photo) sur l'appareil que tu veux connecter. Tu confirmes ici.",
  "settings.qrRequestHint":
    "Scanne ce code avec l'appli Kallisti (ou l'appareil photo) sur un téléphone où tu es connecté, et confirme là-bas.",
  "settings.qrValid": "Le code est valable 2 minutes.",
  "settings.qrAsk": "Se connecter sur {device} ?",
  "settings.qrAskHint": "Seulement si tu as scanné le code toi-même, à l'instant.",
  "settings.qrLogIn": "Se connecter",
  "settings.qrConfirmOn": "Confirme sur {device}…",
  "settings.qrLoggingIn": "Connexion sur {device}…",
  "settings.qrLoggedInOn": "Connecté sur {device}.",
  "settings.qrLoggedIn": "Connecté.",
  "settings.qrRefused": "Non connecté : la connexion a été refusée.",
  "settings.qrExpired": "Le code a expiré.",
  "settings.qrNewCode": "Nouveau code",

  "settings.inputDevice": "Périphérique d'entrée",
  "settings.deviceDefault": "Par défaut",
  "settings.microphone": "Micro",
  "settings.micFellBack": "Le micro choisi n'est pas disponible. Utilisation de {device}.",
  "settings.micFellBackDefault": "Le micro choisi n'est pas disponible. Utilisation du micro par défaut.",
  "settings.micInUse": "Utilisé : {device}",
  "settings.camera": "Caméra",
  "settings.cameraNumbered": "Caméra {number}",
  "settings.cameraListHint": "La liste se remplit une fois que tu as activé la caméra en vocal.",
  "settings.noiseSuppression": "Suppression du bruit",
  "settings.noiseStandard": "Standard (celle du navigateur)",
  "settings.noiseStrong": "Forte (RNNoise)",
  "settings.noiseStrongest": "Maximale (DeepFilterNet3)",
  "settings.noiseOffMusic": "Désactivée en mode musique.",
  "settings.noiseRunning": "{filter} est actif.",
  "settings.noiseLoading": "Chargement de DeepFilterNet3 (environ 12 Mo, une seule fois)…",
  "settings.noiseHint":
    "Forte retire le bruit des ventilateurs et autres bruits constants ; taper au clavier seul ne déclenche pas l'envoi. Maximale retire aussi la plupart des bruits de clavier, mais charge environ 12 Mo, utilise plus de CPU et ajoute un peu de délai.",
  "settings.hearYourself": "T'entendre toi-même",
  "settings.echoTestStart": "Lancer le test d'écho",
  "settings.echoTestStop": "Arrêter",
  "settings.echoTestHint":
    "Joue ton micro comme les autres l'entendent, après la suppression du bruit. Utilise un casque.",
  "settings.outputDevice": "Périphérique de sortie",
  "settings.speakers": "Haut-parleurs",
  "settings.playbackBuffer": "Tampon de lecture",
  "settings.playbackBufferHint":
    "Combien de temps la voix des autres attend avant d'être jouée. Plus long comble les trous sur une mauvaise connexion, mais ajoute du délai. La musique a toujours au moins 500 ms.",
  "settings.inputMode": "Mode d'entrée",
  "settings.voiceActivity": "Détection de la voix",
  "settings.pushToTalk": "Appuyer pour parler",
  "settings.pttKey": "Touche Appuyer pour parler",
  "settings.pttRecording": "Appuie sur une touche… (Échap pour annuler)",
  "settings.pttHintDesktop": "Marche aussi en arrière-plan. La touche est réservée à Kallisti tant que l'appli tourne.",
  "settings.pttHintBrowser": "Marche seulement quand cet onglet a le focus.",
  "settings.keySpace": "Espace",
  "settings.keyCapsLock": "Verr. Maj",
  "settings.keyLeft": "{key} gauche",
  "settings.keyRight": "{key} droite",
  "settings.sensitivity": "Sensibilité",
  "settings.vadThreshold": "Seuil de détection de la voix",
  "settings.decibels": "{value} dB",
  "settings.sensitivityHint":
    "Parle normalement : la barre doit passer au vert quand tu parles, et rester grise quand tu te tais.",
  "settings.echoCancellation": "Annulation de l'écho",
  "settings.echoCancellationHint":
    "Nécessaire avec des haut-parleurs. Avec un casque, tu peux la désactiver : sinon, sur Mac, l'appli de bureau et Safari passent les casques Bluetooth en mode appel, avec un son moins bon.",
  "settings.musicMode": "Mode musique",
  "settings.musicModeHint":
    "Pour jouer de la musique : pas de suppression du bruit, d'annulation de l'écho ni de volume automatique, et un meilleur son (stéréo si le micro a deux canaux). Utilise un casque.",
  "settings.closeMic": "Fermer le micro quand tu es muet",
  "settings.closeMicHint":
    "Le navigateur n'indique alors plus qu'il enregistre, mais le son se coupe un instant quand tu coupes ou réactives le micro.",
  "settings.streamPreviews": "Afficher un aperçu de mon partage d'écran et de ma caméra",
  "settings.streamPreviewsHint": "Les personnes qui ne regardent pas voient une petite image, mise à jour toutes les quelques secondes.",

  "settings.qualityLabel": "{height}p {fps} fps",
  "settings.streamScreen": "Écran de {name}",
  "settings.streamCamera": "Caméra de {name}",
  "settings.obsTitle": "Streamer depuis OBS",
  "settings.obsLive": "Ton stream depuis OBS est en direct.",
  "settings.obsSteps":
    "OBS 30 ou plus récent peut streamer dans ton salon vocal : il s'affiche comme un partage d'écran. Sois connecté ici et dans un salon vocal où tu peux parler. Dans OBS, Paramètres → Stream : Service « WHIP », Serveur {url}, Bearer Token : ta clé de stream.",
  "settings.obsOutput":
    "Paramètres → Sortie (Avancé) : vidéo H.264, intervalle d'images clés 1 ou 2 s, profil baseline ou main, 1080p et 8 Mbit/s au maximum.",
  "settings.obsBFrames":
    "Pas de B-frames (on transmet la vidéo telle quelle ; avec elles, l'image se casse ou devient rose) : avec x264, écris {option} dans « Options x264 » ; avec NVENC, QuickSync ou AMF, mets B-Frames à 0.",
  "settings.obsNewKey": "Ta clé de stream (affichée seulement maintenant) : {key}",
  "settings.obsMakeNewKey": "Créer une nouvelle clé",
  "settings.obsMakeKey": "Créer une clé de stream",
  "settings.obsMaking": "Création…",
  "settings.obsRemoveKey": "Supprimer la clé",
  "settings.obsRemoved": "Supprimée",
  "settings.obsKeyHint":
    "Une nouvelle clé, sa suppression, un nouveau mot de passe ou « Se déconnecter partout » arrêtent l'ancienne clé (et un stream qui l'utilise).",

  "settings.desktopNotifications":
    "Notifications de bureau (mentions et messages privés ; par salon dans son menu)",
  "settings.pushNotifications": "Notifications push sur cet appareil",
  "settings.pushHint":
    "Mentions et messages privés, même quand Kallisti est fermé. Elles disent qui et où, pas le message.",
  "settings.pushInstallHint":
    "Pour les notifications push sur cet iPhone ou iPad : touche Partager, puis « Sur l'écran d'accueil », et ouvre Kallisti depuis là (iOS 16.4 ou plus récent).",
  "settings.pushNotChanged": "Le navigateur ne l'a pas changé.",
  "settings.sounds": "Sons",
  "settings.soundsHint": "Quand tu coupes ton micro ou te mets en sourdine, et quand tu rétablis le son.",
  "settings.soundVolume": "Volume des sons",
  "settings.percent": "{value} %",
  "settings.notifyAll": "Tous les messages",
  "settings.notifyMentions": "Seulement les @mentions",
  "settings.notifyNothing": "Rien",
  "settings.muteFor15Minutes": "Pendant 15 minutes",
  "settings.muteFor1Hour": "Pendant 1 heure",
  "settings.muteFor8Hours": "Pendant 8 heures",
  "settings.muteFor24Hours": "Pendant 24 heures",
  "settings.muteUntilOn": "Jusqu'à ce que je le réactive",

  "settings.accessTitle": "Membres de {name}",
  "settings.accessNote": "Seuls les membres, les admins et les modérateurs voient ce salon.",
  "settings.accessGroupHeading": "{group} — {count}",

  "settings.theme": "Thème",
  "settings.themeDark": "Sombre",
  "settings.themeLight": "Clair",
  "settings.themeSystem": "Comme le système",
  "settings.timeFormat": "Format de l'heure",
  "settings.timeAuto": "Comme la langue",
  "settings.time24": "24 heures (19:05)",
  "settings.time12": "12 heures (7:05 PM)",
} satisfies Translation<typeof en>;
