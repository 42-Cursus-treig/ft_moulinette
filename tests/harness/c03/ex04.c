#include <unistd.h>

char	*ft_strstr(char *str, char *to_find);

static int	my_strlen(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	return (i);
}

int	main(int argc, char **argv)
{
	char	*ret;

	if (argc < 3)
		return (1);
	ret = ft_strstr(argv[1], argv[2]);
	if (ret == (void *)0)
	{
		write(1, "(null)", 6);
		return (0);
	}
	write(1, ret, my_strlen(ret));
	return (0);
}
