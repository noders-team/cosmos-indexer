@NonCPS
def killall_jobs() {
	def jobname = env.JOB_NAME
	def buildnum = env.BUILD_NUMBER.toInteger()
	def killnums = ""
	def job = Jenkins.instance.getItemByFullName(jobname)
	def fixed_job_name = env.JOB_NAME.replace('%2F','/')

	for (build in job.builds) {
		if (!build.isBuilding()) { continue; }
		if (buildnum == build.getNumber().toInteger()) { continue; println "equals" }
		if (buildnum < build.getNumber().toInteger()) { continue; println "newer" }

		echo "Kill task = ${build}"

		killnums += "#" + build.getNumber().toInteger() + ", "

		build.doStop();
	}

	if (killnums != "") {
		slackSend color: "danger", channel: "#jenkins", message: "Killing task(s) ${fixed_job_name} ${killnums} in favor of #${buildnum}, ignore following failed builds for ${killnums}"
	}
	echo "Done killing"
}

pipeline {
    agent {
        label 'BUILD'
    }
    triggers {
        // Запуск при любом пуше в репозиторий (включая теги)
        githubPush()
    }
    options {
        skipDefaultCheckout()  // Отключаем автоматический чекаут
    }

    stages {
        stage('Deploy') {
            steps {
                script {
                    // Чекаут коммита тега
                    checkout([$class: 'GitSCM',
                        branches: [[name: "refs/tags/${env.GIT_TAG_NAME}"]]
                    ])
                }
                buildAndDeployApplication()  // Запуск основного процесса сборки и деплоя
            }
        }
    }
    post {
        success {
            script {
                setBuildStatus("Build complete", "SUCCESS")
            }
        }
        failure {
            script {
                setBuildStatus("Build failed", "FAILURE")
            }
        }
        always {
            cleanUp()
        }
    }
}

void setBuildStatus(String message, String state) {
    step([
            $class: "GitHubCommitStatusSetter",
            reposSource: [$class: "ManuallyEnteredRepositorySource", url: "https://github.com/noders-team/cosmos-indexer.git"],
            contextSource: [$class: "ManuallyEnteredCommitContextSource", context: "ci/jenkins/build-status"],
            errorHandlers: [[$class: "ChangingBuildStatusErrorHandler", result: "SUCCESS"]],
            statusResultSource: [ $class: "ConditionalStatusResultSource", results: [[$class: "AnyBuildResult", message: message, state: state]] ]
    ]);
}

void dockerLogin() {
    withCredentials([usernamePassword(credentialsId: 'jenkins-nexus', passwordVariable: 'pass', usernameVariable: 'user')]) {
        sh script: "docker login --username $user --password $pass ${env.NEXUS_REGISTRY}", label: "Docker login"
    }
}

void buildAndDeployApplication() {
    env.DOCKER_APP = "${JOB_NAME}"
    env.DOCKER_NET_NAME = "vpcbr"
    env.POSTGRES_CONTAINER = "${env.DOCKER_APP}_postgres"
    env.REDIS_CONTAINER = "redis"
    env.GIT_TAG = sh(returnStdout: true, script: "git tag --contains | head -1").trim()
    env.NEXUS_REGISTRY = "nexus.noders.team:5002"

    // Get agents by label
    env.nodes = Jenkins.instance.getLabel('DEPLOY').getNodes().collect { it.getNodeName() }
    def firstString = env.nodes - ~/^\[\s*/
    def lastString = firstString - ~/]\s*$/
    def noCommasString = lastString.split(',').collect { it.trim() }
    noCommasString.each { processedString ->
        withEnv(["env.NODE_NAME=${processedString}"]) {
            // Checks if agent is active or not. Run on active agents
            if (Jenkins.instance.getNode(processedString).toComputer().isOnline()) {
                node(processedString) {
                    echo "${processedString}"
                    env.agent = "${processedString}"
                    checkout scm
                    env.TAG = sh(script: 'git describe --tags', returnStdout: true).trim()
                    echo "The current tag is ${env.TAG}"
                    env.IMAGE_NAME = "${env.NEXUS_REGISTRY}/${env.DOCKER_APP}:${env.TAG}"
                    dockerLogin()
                    sh "docker build -t ${env.IMAGE_NAME} --build-arg TARGETPLATFORM=linux/amd64 ."
                    sh script: "docker push ${env.IMAGE_NAME}"
                    createDockerNetwork()
                    runPostgres()
                    runRedis()
                    runMongo()
                    runApplication()
                }
            }
        }
    }
}

void createDockerNetwork() {
    def networkStatus = sh(script: "docker network ls | grep ${env.DOCKER_NET_NAME} && echo true || echo false", returnStdout: true).trim()
    if (networkStatus.contains("false")) {
        sh script: "docker network create --driver=bridge --subnet=10.5.0.0/16 --gateway=10.5.0.1 ${env.DOCKER_NET_NAME}",
                label: "Create docker network"
    }
}

void runPostgres() {
    def pgStatus = sh(script: "docker ps -a | grep ${env.POSTGRES_CONTAINER} && echo true || echo false", returnStdout: true).trim()
    if (pgStatus.contains("false")) {
        sh """
            docker run -d --name ${env.POSTGRES_CONTAINER} \
                --restart unless-stopped \
                --shm-size=1g \
                -v indexer_postgres:/var/lib/postgresql/data \
                -v /etc/localtime:/etc/localtime:ro \
                -e POSTGRES_USER=taxuser \
                -e POSTGRES_PASSWORD=password \
                -e POSTGRES_DB=postgres \
                -p 5437:5432 \
                --ip 10.5.0.8 \
                --network ${env.DOCKER_NET_NAME} \
                postgres:15-alpine
        """
        // Wait for DB to be ready
        sh """
            while ! docker exec ${env.POSTGRES_CONTAINER} pg_isready -q -h localhost -p 5432 -U postgres; do
                sleep 1
            done
        """
    }
}

void runRedis() {
    def redisStatus = sh(script: "docker ps -a | grep ${env.REDIS_CONTAINER} && echo true || echo false", returnStdout: true).trim()
    if (redisStatus.contains("false")) {
        sh """
            docker run -d --name ${env.REDIS_CONTAINER} \
                --restart unless-stopped \
                -p 6381:6379 \
                --ip 10.5.0.10 \
                --network ${env.DOCKER_NET_NAME} \
                redis
        """
    }
}

void runMongo() {
    def mongoStatus = sh(script: "docker ps -a | grep mongodb && echo true || echo false", returnStdout: true).trim()
    if (mongoStatus.contains("false")) {
        sh """
            docker run -d --name mongodb \
                --restart unless-stopped \
                --shm-size=1g \
                -e MONGO_INITDB_DATABASE=search_indexer \
                -e MONGO_INITDB_ROOT_USERNAME=admin \
                -e MONGO_INITDB_ROOT_PASSWORD=password \
                -v db:/data/db \
                -p 27017-27019:27017-27019 \
                --ip 10.5.0.21 \
                --network ${env.DOCKER_NET_NAME} \
                mongo:5.0.2
        """
    }
}

void runApplication() {
    def appStatus = sh(script: "docker ps -a | grep ${env.DOCKER_APP} && echo true || echo false", returnStdout: true).trim()
    if (env.agent == "celestia") {
        env.baseStartBlock = "1954077"
        env.probeRpc = "https://rpc-celestia.mzonder.com"
        env.probeAccountPrefix = "celestia"
        env.probeChainID = "celestia"
        env.probeChainName = "celestia"
    } else if (env.agent == "dymension") {
        env.baseStartBlock = "1954077"
        env.probeRpc = "http://65.109.54.91:26657"
        env.probeAccountPrefix = "dym"
        env.probeChainID = "dymension_1100-1"
        env.probeChainName = "dymension"
    } else if (env.agent == "nillion") {
        env.baseStartBlock = "10"
        env.probeRpc = "http://159.69.61.113:26657"
        env.probeAccountPrefix = "nillion"
        env.probeChainID = "nillion-chain-testnet-1"
        env.probeChainName = "nillion"
    }
    if (appStatus.contains("true")) {
        sh script: "docker rm -fv ${env.DOCKER_APP}", label: "Remove ${env.DOCKER_APP} container"
    }
    sh """
        docker run -d --name ${env.DOCKER_APP} \
            --restart unless-stopped \
            -p 9002:9002/tcp \
            --network ${env.DOCKER_NET_NAME} \
            --ip 10.5.0.7 \
            --link ${env.POSTGRES_CONTAINER} \
            -v /etc/localtime:/etc/localtime:ro \
            ${env.IMAGE_NAME} \
            /bin/sh -c "/bin/cosmos-indexer index \
              --log.pretty = true \
              --log.level = info \
              --base.start-block ${env.baseStartBlock} \
              --base.end-block -1 \
              --base.throttling 2.005 \
              --base.rpc-workers 10 \
              --base.index-transactions true \
              --base.index-block-events false \
              --probe.rpc ${env.probeRpc} \
              --probe.account-prefix ${env.probeAccountPrefix} \
              --probe.chain-id ${env.probeChainID} \
              --probe.chain-name ${env.probeChainName} \
              --database.host ${env.POSTGRES_CONTAINER} \
              --database.database postgres \
              --database.user taxuser \
              --database.password password \
              --server.port 9002 \
              --redis.addr redis:6379 \
              --mongo.addr mongodb://admin:password@mongodb:27017 \
              --mongo.db search_indexer"
    """
}

void cleanUp() {
    try {
        cleanWs()
        def directories = ["@tmp", "@script", "@script@tmp", "@2", "@2@tmp"]
        directories.each { dirSuffix ->
            dir("${env.WORKSPACE}${dirSuffix}") {
                deleteDir()
            }
        }
    } catch (Exception e) {
        echo 'Error cleaning dirs: ' + e
    }
}

void checkoutBranch(branch) {
    checkout ([$class: 'GitSCM',
               branches: [[ name: "${branch}" ]],
               userRemoteConfigs: [[
               credentialsId: 'robert_gh',
               url: "https://github.com/noders-team/cosmos-indexer.git"]]])
}
